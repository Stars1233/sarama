package sarama

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rcrowley/go-metrics"
)

// ConsumerMessage encapsulates a Kafka message returned by the consumer.
type ConsumerMessage struct {
	Headers        []*RecordHeader // only set if kafka is version 0.11+
	Timestamp      time.Time       // only set if kafka is version 0.10+, inner message timestamp
	BlockTimestamp time.Time       // only set if kafka is version 0.10+, outer (compressed) block timestamp

	Key, Value []byte
	Topic      string
	Partition  int32
	Offset     int64
}

// ConsumerError is what is provided to the user when an error occurs.
// It wraps an error and includes the topic and partition.
type ConsumerError struct {
	Topic     string
	Partition int32
	Err       error
}

func (ce ConsumerError) Error() string {
	return fmt.Sprintf("kafka: error while consuming %s/%d: %s", ce.Topic, ce.Partition, ce.Err)
}

func (ce ConsumerError) Unwrap() error {
	return ce.Err
}

// ConsumerErrors is a type that wraps a batch of errors and implements the Error interface.
// It can be returned from the PartitionConsumer's Close methods to avoid the need to manually drain errors
// when stopping.
type ConsumerErrors []*ConsumerError

func (ce ConsumerErrors) Error() string {
	return fmt.Sprintf("kafka: %d errors while consuming", len(ce))
}

// Consumer manages PartitionConsumers which process Kafka messages from brokers. You MUST call Close()
// on a consumer to avoid leaks, it will not be garbage-collected automatically when it passes out of
// scope.
type Consumer interface {
	// Topics returns the set of available topics as retrieved from the cluster
	// metadata. This method is the same as Client.Topics(), and is provided for
	// convenience.
	Topics() ([]string, error)

	// Partitions returns the sorted list of all partition IDs for the given topic.
	// This method is the same as Client.Partitions(), and is provided for convenience.
	Partitions(topic string) ([]int32, error)

	// ConsumePartition creates a PartitionConsumer on the given topic/partition with
	// the given offset. It will return an error if this Consumer is already consuming
	// on the given topic/partition. Offset can be a literal offset, or OffsetNewest
	// or OffsetOldest
	ConsumePartition(topic string, partition int32, offset int64) (PartitionConsumer, error)

	// HighWaterMarks returns the current high water marks for each topic and partition.
	// Consistency between partitions is not guaranteed since high water marks are updated separately.
	HighWaterMarks() map[string]map[int32]int64

	// Close shuts down the consumer. It must be called after all child
	// PartitionConsumers have already been closed.
	Close() error

	// Pause suspends fetching from the requested partitions. Future calls to the broker will not return any
	// records from these partitions until they have been resumed using Resume()/ResumeAll().
	// Note that this method does not affect partition subscription.
	// In particular, it does not cause a group rebalance when automatic assignment is used.
	Pause(topicPartitions map[string][]int32)

	// Resume resumes specified partitions which have been paused with Pause()/PauseAll().
	// New calls to the broker will return records from these partitions if there are any to be fetched.
	Resume(topicPartitions map[string][]int32)

	// PauseAll suspends fetching from all partitions. Future calls to the broker will not return any
	// records from these partitions until they have been resumed using Resume()/ResumeAll().
	// Note that this method does not affect partition subscription.
	// In particular, it does not cause a group rebalance when automatic assignment is used.
	PauseAll()

	// ResumeAll resumes all partitions which have been paused with Pause()/PauseAll().
	// New calls to the broker will return records from these partitions if there are any to be fetched.
	ResumeAll()
}

// max time to wait for more partition subscriptions
const partitionConsumersBatchTimeout = 100 * time.Millisecond

type consumer struct {
	conf            *Config
	children        map[string]map[int32]*partitionConsumer
	brokerConsumers map[*Broker]*brokerConsumer
	client          Client
	metricRegistry  metrics.Registry
	lock            sync.Mutex
}

// NewConsumer creates a new consumer using the given broker addresses and configuration.
func NewConsumer(addrs []string, config *Config) (Consumer, error) {
	client, err := NewClient(addrs, config)
	if err != nil {
		return nil, err
	}
	return newConsumer(client)
}

// NewConsumerFromClient creates a new consumer using the given client. It is still
// necessary to call Close() on the underlying client when shutting down this consumer.
func NewConsumerFromClient(client Client) (Consumer, error) {
	// For clients passed in by the client, ensure we don't
	// call Close() on it.
	cli := &nopCloserClient{client}
	return newConsumer(cli)
}

func newConsumer(client Client) (Consumer, error) {
	// Check that we are not dealing with a closed Client before processing any other arguments
	if client.Closed() {
		return nil, ErrClosedClient
	}

	c := &consumer{
		client:          client,
		conf:            client.Config(),
		children:        make(map[string]map[int32]*partitionConsumer),
		brokerConsumers: make(map[*Broker]*brokerConsumer),
		metricRegistry:  newCleanupRegistry(client.Config().MetricRegistry),
	}

	return c, nil
}

func (c *consumer) Close() error {
	c.metricRegistry.UnregisterAll()
	return c.client.Close()
}

func (c *consumer) Topics() ([]string, error) {
	return c.client.Topics()
}

func (c *consumer) Partitions(topic string) ([]int32, error) {
	return c.client.Partitions(topic)
}

func (c *consumer) ConsumePartition(topic string, partition int32, offset int64) (PartitionConsumer, error) {
	child := &partitionConsumer{
		consumer:             c,
		conf:                 c.conf,
		topic:                topic,
		partition:            partition,
		messages:             make(chan *ConsumerMessage, c.conf.ChannelBufferSize),
		errors:               make(chan *ConsumerError, c.conf.ChannelBufferSize),
		feeder:               make(chan *FetchResponse, 1),
		leaderEpoch:          invalidLeaderEpoch,
		preferredReadReplica: invalidPreferredReplicaID,
		trigger:              make(chan none, 1),
		dying:                make(chan none),
		fetchSize:            c.conf.Consumer.Fetch.Default,
	}

	if err := child.chooseStartingOffset(offset); err != nil {
		return nil, err
	}

	leader, epoch, err := c.client.LeaderAndEpoch(child.topic, child.partition)
	if err != nil {
		return nil, err
	}

	if err := c.addChild(child); err != nil {
		return nil, err
	}

	go withRecover(child.dispatcher)
	go withRecover(child.responseFeeder)

	child.leaderEpoch = epoch
	child.broker = c.refBrokerConsumer(leader)
	child.broker.input <- child

	return child, nil
}

func (c *consumer) HighWaterMarks() map[string]map[int32]int64 {
	c.lock.Lock()
	defer c.lock.Unlock()

	hwms := make(map[string]map[int32]int64)
	for topic, p := range c.children {
		hwm := make(map[int32]int64, len(p))
		for partition, pc := range p {
			hwm[partition] = pc.HighWaterMarkOffset()
		}
		hwms[topic] = hwm
	}

	return hwms
}

func (c *consumer) addChild(child *partitionConsumer) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	topicChildren := c.children[child.topic]
	if topicChildren == nil {
		topicChildren = make(map[int32]*partitionConsumer)
		c.children[child.topic] = topicChildren
	}

	if topicChildren[child.partition] != nil {
		return ConfigurationError("That topic/partition is already being consumed")
	}

	topicChildren[child.partition] = child
	return nil
}

func (c *consumer) removeChild(child *partitionConsumer) {
	c.lock.Lock()
	defer c.lock.Unlock()

	delete(c.children[child.topic], child.partition)
}

func (c *consumer) refBrokerConsumer(broker *Broker) *brokerConsumer {
	c.lock.Lock()
	defer c.lock.Unlock()

	bc := c.brokerConsumers[broker]
	if bc == nil {
		bc = c.newBrokerConsumer(broker)
		c.brokerConsumers[broker] = bc
	}

	bc.refs++

	return bc
}

func (c *consumer) unrefBrokerConsumer(brokerWorker *brokerConsumer) {
	c.lock.Lock()
	defer c.lock.Unlock()

	brokerWorker.refs--

	if brokerWorker.refs == 0 {
		close(brokerWorker.input)
		if c.brokerConsumers[brokerWorker.broker] == brokerWorker {
			delete(c.brokerConsumers, brokerWorker.broker)
		}
	}
}

func (c *consumer) abandonBrokerConsumer(brokerWorker *brokerConsumer) {
	c.lock.Lock()
	defer c.lock.Unlock()

	delete(c.brokerConsumers, brokerWorker.broker)
}

// Pause implements Consumer.
func (c *consumer) Pause(topicPartitions map[string][]int32) {
	c.lock.Lock()
	defer c.lock.Unlock()

	for topic, partitions := range topicPartitions {
		for _, partition := range partitions {
			if topicConsumers, ok := c.children[topic]; ok {
				if partitionConsumer, ok := topicConsumers[partition]; ok {
					partitionConsumer.Pause()
				}
			}
		}
	}
}

// Resume implements Consumer.
func (c *consumer) Resume(topicPartitions map[string][]int32) {
	c.lock.Lock()
	defer c.lock.Unlock()

	for topic, partitions := range topicPartitions {
		for _, partition := range partitions {
			if topicConsumers, ok := c.children[topic]; ok {
				if partitionConsumer, ok := topicConsumers[partition]; ok {
					partitionConsumer.Resume()
				}
			}
		}
	}
}

// PauseAll implements Consumer.
func (c *consumer) PauseAll() {
	c.lock.Lock()
	defer c.lock.Unlock()

	for _, partitions := range c.children {
		for _, partitionConsumer := range partitions {
			partitionConsumer.Pause()
		}
	}
}

// ResumeAll implements Consumer.
func (c *consumer) ResumeAll() {
	c.lock.Lock()
	defer c.lock.Unlock()

	for _, partitions := range c.children {
		for _, partitionConsumer := range partitions {
			partitionConsumer.Resume()
		}
	}
}

// PartitionConsumer

// PartitionConsumer processes Kafka messages from a given topic and partition. You MUST call one of Close() or
// AsyncClose() on a PartitionConsumer to avoid leaks; it will not be garbage-collected automatically when it passes out
// of scope.
//
// The simplest way of using a PartitionConsumer is to loop over its Messages channel using a for/range
// loop. The PartitionConsumer will only stop itself in one case: when the offset being consumed is reported
// as out of range by the brokers. In this case you should decide what you want to do (try a different offset,
// notify a human, etc) and handle it appropriately. For all other error cases, it will just keep retrying.
// By default, it logs these errors to sarama.Logger; if you want to be notified directly of all errors, set
// your config's Consumer.Return.Errors to true and read from the Errors channel, using a select statement
// or a separate goroutine. Check out the Consumer examples to see implementations of these different approaches.
//
// To terminate such a for/range loop while the loop is executing, call AsyncClose. This will kick off the process of
// consumer tear-down & return immediately. Continue to loop, servicing the Messages channel until the teardown process
// AsyncClose initiated closes it (thus terminating the for/range loop). If you've already ceased reading Messages, call
// Close; this will signal the PartitionConsumer's goroutines to begin shutting down (just like AsyncClose), but will
// also drain the Messages channel, harvest all errors & return them once cleanup has completed.
type PartitionConsumer interface {
	// AsyncClose initiates a shutdown of the PartitionConsumer. This method will return immediately, after which you
	// should continue to service the 'Messages' and 'Errors' channels until they are empty. It is required to call this
	// function, or Close before a consumer object passes out of scope, as it will otherwise leak memory. You must call
	// this before calling Close on the underlying client.
	AsyncClose()

	// Close stops the PartitionConsumer from fetching messages. It will initiate a shutdown just like AsyncClose, drain
	// the Messages channel, harvest any errors & return them to the caller. Note that if you are continuing to service
	// the Messages channel when this function is called, you will be competing with Close for messages; consider
	// calling AsyncClose, instead. It is required to call this function (or AsyncClose) before a consumer object passes
	// out of scope, as it will otherwise leak memory. You must call this before calling Close on the underlying client.
	Close() error

	// Messages returns the read channel for the messages that are returned by
	// the broker.
	Messages() <-chan *ConsumerMessage

	// Errors returns a read channel of errors that occurred during consuming, if
	// enabled. By default, errors are logged and not returned over this channel.
	// If you want to implement any custom error handling, set your config's
	// Consumer.Return.Errors setting to true, and read from this channel.
	Errors() <-chan *ConsumerError

	// HighWaterMarkOffset returns the high water mark offset of the partition,
	// i.e. the offset that will be used for the next message that will be produced.
	// You can use this to determine how far behind the processing is.
	HighWaterMarkOffset() int64

	// Pause suspends fetching from this partition. Future calls to the broker will not return
	// any records from these partition until it have been resumed using Resume().
	// Note that this method does not affect partition subscription.
	// In particular, it does not cause a group rebalance when automatic assignment is used.
	Pause()

	// Resume resumes this partition which have been paused with Pause().
	// New calls to the broker will return records from these partitions if there are any to be fetched.
	// If the partition was not previously paused, this method is a no-op.
	Resume()

	// IsPaused indicates if this partition consumer is paused or not
	IsPaused() bool
}

type partitionConsumer struct {
	highWaterMarkOffset int64 // must be at the top of the struct because https://golang.org/pkg/sync/atomic/#pkg-note-BUG

	consumer *consumer
	conf     *Config
	broker   *brokerConsumer
	messages chan *ConsumerMessage
	errors   chan *ConsumerError
	feeder   chan *FetchResponse

	leaderEpoch          int32
	preferredReadReplica int32

	trigger, dying chan none
	closeOnce      sync.Once
	topic          string
	partition      int32
	responseResult error
	fetchSize      int32
	offset         int64
	retries        int32

	paused int32
}

var errTimedOut = errors.New("timed out feeding messages to the user") // not user-facing

func (child *partitionConsumer) sendError(err error) {
	cErr := &ConsumerError{
		Topic:     child.topic,
		Partition: child.partition,
		Err:       err,
	}

	if child.conf.Consumer.Return.Errors {
		child.errors <- cErr
	} else {
		Logger.Println(cErr)
	}
}

func (child *partitionConsumer) computeBackoff() time.Duration {
	if child.conf.Consumer.Retry.BackoffFunc != nil {
		retries := atomic.AddInt32(&child.retries, 1)
		return child.conf.Consumer.Retry.BackoffFunc(int(retries))
	}
	return child.conf.Consumer.Retry.Backoff
}

func (child *partitionConsumer) dispatcher() {
	for range child.trigger {
		select {
		case <-child.dying:
			close(child.trigger)
		case <-time.After(child.computeBackoff()):
			if child.broker != nil {
				child.consumer.unrefBrokerConsumer(child.broker)
				child.broker = nil
			}

			if err := child.dispatch(); err != nil {
				child.sendError(err)
				child.trigger <- none{}
			}
		}
	}

	if child.broker != nil {
		child.consumer.unrefBrokerConsumer(child.broker)
	}
	child.consumer.removeChild(child)
	close(child.feeder)
}

func (child *partitionConsumer) preferredBroker() (*Broker, int32, error) {
	if child.preferredReadReplica >= 0 {
		broker, err := child.consumer.client.Broker(child.preferredReadReplica)
		if err == nil {
			return broker, child.leaderEpoch, nil
		}
		Logger.Printf(
			"consumer/%s/%d failed to find active broker for preferred read replica %d - will fallback to leader",
			child.topic, child.partition, child.preferredReadReplica)

		// if we couldn't find it, discard the replica preference and trigger a
		// metadata refresh whilst falling back to consuming from the leader again
		child.preferredReadReplica = invalidPreferredReplicaID
		_ = child.consumer.client.RefreshMetadata(child.topic)
	}

	// if preferred replica cannot be found fallback to leader
	return child.consumer.client.LeaderAndEpoch(child.topic, child.partition)
}

func (child *partitionConsumer) dispatch() error {
	if err := child.consumer.client.RefreshMetadata(child.topic); err != nil {
		return err
	}

	broker, epoch, err := child.preferredBroker()
	if err != nil {
		return err
	}

	child.leaderEpoch = epoch
	child.broker = child.consumer.refBrokerConsumer(broker)
	child.broker.input <- child

	return nil
}

func (child *partitionConsumer) chooseStartingOffset(offset int64) error {
	newestOffset, err := child.consumer.client.GetOffset(child.topic, child.partition, OffsetNewest)
	if err != nil {
		return err
	}

	child.highWaterMarkOffset = newestOffset

	oldestOffset, err := child.consumer.client.GetOffset(child.topic, child.partition, OffsetOldest)
	if err != nil {
		return err
	}

	switch {
	case offset == OffsetNewest:
		child.offset = newestOffset
	case offset == OffsetOldest:
		child.offset = oldestOffset
	case offset >= oldestOffset && offset <= newestOffset:
		child.offset = offset
	default:
		return ErrOffsetOutOfRange
	}

	return nil
}

func (child *partitionConsumer) Messages() <-chan *ConsumerMessage {
	return child.messages
}

func (child *partitionConsumer) Errors() <-chan *ConsumerError {
	return child.errors
}

func (child *partitionConsumer) AsyncClose() {
	// this triggers whatever broker owns this child to abandon it and close its trigger channel, which causes
	// the dispatcher to exit its loop, which removes it from the consumer then closes its 'messages' and
	// 'errors' channel (alternatively, if the child is already at the dispatcher for some reason, that will
	// also just close itself)
	child.closeOnce.Do(func() {
		close(child.dying)
	})
}

func (child *partitionConsumer) Close() error {
	child.AsyncClose()

	var consumerErrors ConsumerErrors
	for err := range child.errors {
		consumerErrors = append(consumerErrors, err)
	}

	if len(consumerErrors) > 0 {
		return consumerErrors
	}
	return nil
}

func (child *partitionConsumer) HighWaterMarkOffset() int64 {
	return atomic.LoadInt64(&child.highWaterMarkOffset)
}

func (child *partitionConsumer) responseFeeder() {
	var msgs []*ConsumerMessage
	expiryTicker := time.NewTicker(child.conf.Consumer.MaxProcessingTime)
	firstAttempt := true

feederLoop:
	for response := range child.feeder {
		msgs, child.responseResult = child.parseResponse(response)

		if child.responseResult == nil {
			atomic.StoreInt32(&child.retries, 0)
		}

		for i, msg := range msgs {
			child.interceptors(msg)
		messageSelect:
			select {
			case <-child.dying:
				child.broker.acks.Done()
				continue feederLoop
			case child.messages <- msg:
				firstAttempt = true
			case <-expiryTicker.C:
				if !firstAttempt {
					child.responseResult = errTimedOut
					child.broker.acks.Done()
				remainingLoop:
					for _, msg = range msgs[i:] {
						child.interceptors(msg)
						select {
						case child.messages <- msg:
						case <-child.dying:
							break remainingLoop
						}
					}
					child.broker.input <- child
					continue feederLoop
				} else {
					// current message has not been sent, return to select
					// statement
					firstAttempt = false
					goto messageSelect
				}
			}
		}

		child.broker.acks.Done()
	}

	expiryTicker.Stop()
	close(child.messages)
	close(child.errors)
}

func (child *partitionConsumer) parseMessages(msgSet *MessageSet) ([]*ConsumerMessage, error) {
	var messages []*ConsumerMessage
	for _, msgBlock := range msgSet.Messages {
		for _, msg := range msgBlock.Messages() {
			offset := msg.Offset
			timestamp := msg.Msg.Timestamp
			if msg.Msg.Version >= 1 {
				baseOffset := msgBlock.Offset - msgBlock.Messages()[len(msgBlock.Messages())-1].Offset
				offset += baseOffset
				if msg.Msg.LogAppendTime {
					timestamp = msgBlock.Msg.Timestamp
				}
			}
			if offset < child.offset {
				continue
			}
			messages = append(messages, &ConsumerMessage{
				Topic:          child.topic,
				Partition:      child.partition,
				Key:            msg.Msg.Key,
				Value:          msg.Msg.Value,
				Offset:         offset,
				Timestamp:      timestamp,
				BlockTimestamp: msgBlock.Msg.Timestamp,
			})
			child.offset = offset + 1
		}
	}
	if len(messages) == 0 {
		child.offset++
	}
	return messages, nil
}

func (child *partitionConsumer) parseRecords(batch *RecordBatch) ([]*ConsumerMessage, error) {
	messages := make([]*ConsumerMessage, 0, len(batch.Records))

	for _, rec := range batch.Records {
		offset := batch.FirstOffset + rec.OffsetDelta
		if offset < child.offset {
			continue
		}
		timestamp := batch.FirstTimestamp.Add(rec.TimestampDelta)
		if batch.LogAppendTime {
			timestamp = batch.MaxTimestamp
		}
		messages = append(messages, &ConsumerMessage{
			Topic:     child.topic,
			Partition: child.partition,
			Key:       rec.Key,
			Value:     rec.Value,
			Offset:    offset,
			Timestamp: timestamp,
			Headers:   rec.Headers,
		})
		child.offset = offset + 1
	}
	if len(messages) == 0 {
		child.offset++
	}
	return messages, nil
}

func (child *partitionConsumer) parseResponse(response *FetchResponse) ([]*ConsumerMessage, error) {
	var consumerBatchSizeMetric metrics.Histogram
	if child.consumer != nil && child.consumer.metricRegistry != nil {
		consumerBatchSizeMetric = getOrRegisterHistogram("consumer-batch-size", child.consumer.metricRegistry)
	}

	// If request was throttled and empty we log and return without error
	if response.ThrottleTime != time.Duration(0) && len(response.Blocks) == 0 {
		Logger.Printf(
			"consumer/broker/%d FetchResponse throttled %v\n",
			child.broker.broker.ID(), response.ThrottleTime)
		return nil, nil
	}

	block := response.GetBlock(child.topic, child.partition)
	if block == nil {
		return nil, ErrIncompleteResponse
	}

	if !errors.Is(block.Err, ErrNoError) {
		return nil, block.Err
	}

	nRecs, err := block.numRecords()
	if err != nil {
		return nil, err
	}

	if consumerBatchSizeMetric != nil {
		consumerBatchSizeMetric.Update(int64(nRecs))
	}

	if block.PreferredReadReplica != invalidPreferredReplicaID {
		child.preferredReadReplica = block.PreferredReadReplica
	}

	if nRecs == 0 {
		partialTrailingMessage, err := block.isPartial()
		if err != nil {
			return nil, err
		}
		// We got no messages. If we got a trailing one then we need to ask for more data.
		// Otherwise we just poll again and wait for one to be produced...
		if partialTrailingMessage {
			if child.conf.Consumer.Fetch.Max > 0 && child.fetchSize == child.conf.Consumer.Fetch.Max {
				// we can't ask for more data, we've hit the configured limit
				child.sendError(ErrMessageTooLarge)
				child.offset++ // skip this one so we can keep processing future messages
			} else {
				child.fetchSize *= 2
				// check int32 overflow
				if child.fetchSize < 0 {
					child.fetchSize = math.MaxInt32
				}
				if child.conf.Consumer.Fetch.Max > 0 && child.fetchSize > child.conf.Consumer.Fetch.Max {
					child.fetchSize = child.conf.Consumer.Fetch.Max
				}
			}
		} else if block.recordsNextOffset != nil && *block.recordsNextOffset <= block.HighWaterMarkOffset {
			// check last record next offset to avoid stuck if high watermark was not reached
			Logger.Printf("consumer/broker/%d received batch with zero records but high watermark was not reached, topic %s, partition %d, next offset %d\n", child.broker.broker.ID(), child.topic, child.partition, *block.recordsNextOffset)
			child.offset = *block.recordsNextOffset
		}

		return nil, nil
	}

	// we got messages, reset our fetch size in case it was increased for a previous request
	child.fetchSize = child.conf.Consumer.Fetch.Default
	atomic.StoreInt64(&child.highWaterMarkOffset, block.HighWaterMarkOffset)

	// abortedProducerIDs contains producerID which message should be ignored as uncommitted
	// - producerID are added when the partitionConsumer iterate over the offset at which an aborted transaction begins (abortedTransaction.FirstOffset)
	// - producerID are removed when partitionConsumer iterate over an aborted controlRecord, meaning the aborted transaction for this producer is over
	abortedProducerIDs := make(map[int64]struct{}, len(block.AbortedTransactions))
	abortedTransactions := block.getAbortedTransactions()

	var messages []*ConsumerMessage
	for _, records := range block.RecordsSet {
		switch records.recordsType {
		case legacyRecords:
			messageSetMessages, err := child.parseMessages(records.MsgSet)
			if err != nil {
				return nil, err
			}

			messages = append(messages, messageSetMessages...)
		case defaultRecords:
			// Consume remaining abortedTransaction up to last offset of current batch
			for _, txn := range abortedTransactions {
				if txn.FirstOffset > records.RecordBatch.LastOffset() {
					break
				}
				abortedProducerIDs[txn.ProducerID] = struct{}{}
				// Pop abortedTransactions so that we never add it again
				abortedTransactions = abortedTransactions[1:]
			}

			recordBatchMessages, err := child.parseRecords(records.RecordBatch)
			if err != nil {
				return nil, err
			}

			// Parse and commit offset but do not expose messages that are:
			// - control records
			// - part of an aborted transaction when set to `ReadCommitted`

			// control record
			isControl, err := records.isControl()
			if err != nil {
				// I don't know why there is this continue in case of error to begin with
				// Safe bet is to ignore control messages if ReadUncommitted
				// and block on them in case of error and ReadCommitted
				if child.conf.Consumer.IsolationLevel == ReadCommitted {
					return nil, err
				}
				continue
			}
			if isControl {
				controlRecord, err := records.getControlRecord()
				if err != nil {
					return nil, err
				}

				if controlRecord.Type == ControlRecordAbort {
					delete(abortedProducerIDs, records.RecordBatch.ProducerID)
				}
				continue
			}

			// filter aborted transactions
			if child.conf.Consumer.IsolationLevel == ReadCommitted {
				_, isAborted := abortedProducerIDs[records.RecordBatch.ProducerID]
				if records.RecordBatch.IsTransactional && isAborted {
					continue
				}
			}

			messages = append(messages, recordBatchMessages...)
		default:
			return nil, fmt.Errorf("unknown records type: %v", records.recordsType)
		}
	}

	return messages, nil
}

func (child *partitionConsumer) interceptors(msg *ConsumerMessage) {
	for _, interceptor := range child.conf.Consumer.Interceptors {
		msg.safelyApplyInterceptor(interceptor)
	}
}

// Pause implements PartitionConsumer.
func (child *partitionConsumer) Pause() {
	atomic.StoreInt32(&child.paused, 1)
}

// Resume implements PartitionConsumer.
func (child *partitionConsumer) Resume() {
	atomic.StoreInt32(&child.paused, 0)
}

// IsPaused implements PartitionConsumer.
func (child *partitionConsumer) IsPaused() bool {
	return atomic.LoadInt32(&child.paused) == 1
}

type brokerConsumer struct {
	consumer         *consumer
	broker           *Broker
	input            chan *partitionConsumer
	newSubscriptions chan []*partitionConsumer
	subscriptions    map[*partitionConsumer]none
	acks             sync.WaitGroup
	refs             int
}

func (c *consumer) newBrokerConsumer(broker *Broker) *brokerConsumer {
	bc := &brokerConsumer{
		consumer:         c,
		broker:           broker,
		input:            make(chan *partitionConsumer),
		newSubscriptions: make(chan []*partitionConsumer),
		subscriptions:    make(map[*partitionConsumer]none),
		refs:             0,
	}

	go withRecover(bc.subscriptionManager)
	go withRecover(bc.subscriptionConsumer)

	return bc
}

// The subscriptionManager constantly accepts new subscriptions on `input` (even when the main subscriptionConsumer
// goroutine is in the middle of a network request) and batches it up. The main worker goroutine picks
// up a batch of new subscriptions between every network request by reading from `newSubscriptions`, so we give
// it nil if no new subscriptions are available.
func (bc *brokerConsumer) subscriptionManager() {
	defer close(bc.newSubscriptions)

	for {
		var partitionConsumers []*partitionConsumer

		// Check for any partition consumer asking to subscribe if there aren't
		// any, trigger the network request (to fetch Kafka messages) by sending "nil" to the
		// newSubscriptions channel
		select {
		case pc, ok := <-bc.input:
			if !ok {
				return
			}
			partitionConsumers = append(partitionConsumers, pc)
		case bc.newSubscriptions <- nil:
			continue
		}

		// drain input of any further incoming subscriptions
		timer := time.NewTimer(partitionConsumersBatchTimeout)
		for batchComplete := false; !batchComplete; {
			select {
			case pc := <-bc.input:
				partitionConsumers = append(partitionConsumers, pc)
			case <-timer.C:
				batchComplete = true
			}
		}
		timer.Stop()

		Logger.Printf(
			"consumer/broker/%d accumulated %d new subscriptions\n",
			bc.broker.ID(), len(partitionConsumers))

		bc.newSubscriptions <- partitionConsumers
	}
}

// subscriptionConsumer ensures we will get nil right away if no new subscriptions is available
// this is the main loop that fetches Kafka messages
func (bc *brokerConsumer) subscriptionConsumer() {
	for newSubscriptions := range bc.newSubscriptions {
		bc.updateSubscriptions(newSubscriptions)

		if len(bc.subscriptions) == 0 {
			// We're about to be shut down or we're about to receive more subscriptions.
			// Take a small nap to avoid burning the CPU.
			time.Sleep(partitionConsumersBatchTimeout)
			continue
		}

		response, err := bc.fetchNewMessages()
		if err != nil {
			Logger.Printf("consumer/broker/%d disconnecting due to error processing FetchRequest: %s\n", bc.broker.ID(), err)
			bc.abort(err)
			return
		}

		// if there isn't response, it means that not fetch was made
		// so we don't need to handle any response
		if response == nil {
			time.Sleep(partitionConsumersBatchTimeout)
			continue
		}

		bc.acks.Add(len(bc.subscriptions))
		for child := range bc.subscriptions {
			if _, ok := response.Blocks[child.topic]; !ok {
				bc.acks.Done()
				continue
			}

			if _, ok := response.Blocks[child.topic][child.partition]; !ok {
				bc.acks.Done()
				continue
			}

			child.feeder <- response
		}
		bc.acks.Wait()
		bc.handleResponses()
	}
}

func (bc *brokerConsumer) updateSubscriptions(newSubscriptions []*partitionConsumer) {
	for _, child := range newSubscriptions {
		bc.subscriptions[child] = none{}
		Logger.Printf("consumer/broker/%d added subscription to %s/%d\n", bc.broker.ID(), child.topic, child.partition)
	}

	for child := range bc.subscriptions {
		select {
		case <-child.dying:
			Logger.Printf("consumer/broker/%d closed dead subscription to %s/%d\n", bc.broker.ID(), child.topic, child.partition)
			close(child.trigger)
			delete(bc.subscriptions, child)
		default:
			// no-op
		}
	}
}

// handleResponses handles the response codes left for us by our subscriptions, and abandons ones that have been closed
func (bc *brokerConsumer) handleResponses() {
	for child := range bc.subscriptions {
		result := child.responseResult
		child.responseResult = nil

		if result == nil {
			if preferredBroker, _, err := child.preferredBroker(); err == nil {
				if bc.broker.ID() != preferredBroker.ID() {
					// not an error but needs redispatching to consume from preferred replica
					Logger.Printf(
						"consumer/broker/%d abandoned in favor of preferred replica broker/%d\n",
						bc.broker.ID(), preferredBroker.ID())
					child.trigger <- none{}
					delete(bc.subscriptions, child)
				}
			}
			continue
		}

		// Discard any replica preference.
		child.preferredReadReplica = invalidPreferredReplicaID

		if errors.Is(result, errTimedOut) {
			Logger.Printf("consumer/broker/%d abandoned subscription to %s/%d because consuming was taking too long\n",
				bc.broker.ID(), child.topic, child.partition)
			delete(bc.subscriptions, child)
		} else if errors.Is(result, ErrOffsetOutOfRange) {
			// there's no point in retrying this it will just fail the same way again
			// shut it down and force the user to choose what to do
			child.sendError(result)
			Logger.Printf("consumer/%s/%d shutting down because %s\n", child.topic, child.partition, result)
			close(child.trigger)
			delete(bc.subscriptions, child)
		} else if errors.Is(result, ErrUnknownTopicOrPartition) ||
			errors.Is(result, ErrNotLeaderForPartition) ||
			errors.Is(result, ErrLeaderNotAvailable) ||
			errors.Is(result, ErrReplicaNotAvailable) ||
			errors.Is(result, ErrFencedLeaderEpoch) ||
			errors.Is(result, ErrUnknownLeaderEpoch) {
			// not an error, but does need redispatching
			Logger.Printf("consumer/broker/%d abandoned subscription to %s/%d because %s\n",
				bc.broker.ID(), child.topic, child.partition, result)
			child.trigger <- none{}
			delete(bc.subscriptions, child)
		} else {
			// dunno, tell the user and try redispatching
			child.sendError(result)
			Logger.Printf("consumer/broker/%d abandoned subscription to %s/%d because %s\n",
				bc.broker.ID(), child.topic, child.partition, result)
			child.trigger <- none{}
			delete(bc.subscriptions, child)
		}
	}
}

func (bc *brokerConsumer) abort(err error) {
	bc.consumer.abandonBrokerConsumer(bc)
	_ = bc.broker.Close() // we don't care about the error this might return, we already have one

	for child := range bc.subscriptions {
		child.sendError(err)
		child.trigger <- none{}
	}

	for newSubscriptions := range bc.newSubscriptions {
		if len(newSubscriptions) == 0 {
			// Take a small nap to avoid burning the CPU.
			time.Sleep(partitionConsumersBatchTimeout)
			continue
		}
		for _, child := range newSubscriptions {
			child.sendError(err)
			child.trigger <- none{}
		}
	}
}

// fetchResponse can be nil if no fetch is made, it can occur when
// all partitions are paused
func (bc *brokerConsumer) fetchNewMessages() (*FetchResponse, error) {
	request := &FetchRequest{
		MinBytes:    bc.consumer.conf.Consumer.Fetch.Min,
		MaxWaitTime: int32(bc.consumer.conf.Consumer.MaxWaitTime / time.Millisecond),
	}
	// Version 1 is the same as version 0.
	if bc.consumer.conf.Version.IsAtLeast(V0_9_0_0) {
		request.Version = 1
	}
	// Starting in Version 2, the requestor must be able to handle Kafka Log
	// Message format version 1.
	if bc.consumer.conf.Version.IsAtLeast(V0_10_0_0) {
		request.Version = 2
	}
	// Version 3 adds MaxBytes.  Starting in version 3, the partition ordering in
	// the request is now relevant.  Partitions will be processed in the order
	// they appear in the request.
	if bc.consumer.conf.Version.IsAtLeast(V0_10_1_0) {
		request.Version = 3
		request.MaxBytes = MaxResponseSize
	}
	// Version 4 adds IsolationLevel.  Starting in version 4, the reqestor must be
	// able to handle Kafka log message format version 2.
	// Version 5 adds LogStartOffset to indicate the earliest available offset of
	// partition data that can be consumed.
	if bc.consumer.conf.Version.IsAtLeast(V0_11_0_0) {
		request.Version = 5
		request.Isolation = bc.consumer.conf.Consumer.IsolationLevel
	}
	// Version 6 is the same as version 5.
	if bc.consumer.conf.Version.IsAtLeast(V1_0_0_0) {
		request.Version = 6
	}
	// Version 7 adds incremental fetch request support.
	if bc.consumer.conf.Version.IsAtLeast(V1_1_0_0) {
		request.Version = 7
		// We do not currently implement KIP-227 FetchSessions. Setting the id to 0
		// and the epoch to -1 tells the broker not to generate as session ID we're going
		// to just ignore anyway.
		request.SessionID = 0
		request.SessionEpoch = -1
	}
	// Version 8 is the same as version 7.
	if bc.consumer.conf.Version.IsAtLeast(V2_0_0_0) {
		request.Version = 8
	}
	// Version 9 adds CurrentLeaderEpoch, as described in KIP-320.
	// Version 10 indicates that we can use the ZStd compression algorithm, as
	// described in KIP-110.
	if bc.consumer.conf.Version.IsAtLeast(V2_1_0_0) {
		request.Version = 10
	}
	// Version 11 adds RackID for KIP-392 fetch from closest replica
	if bc.consumer.conf.Version.IsAtLeast(V2_3_0_0) {
		request.Version = 11
		request.RackID = bc.consumer.conf.RackID
	}

	for child := range bc.subscriptions {
		if !child.IsPaused() {
			request.AddBlock(child.topic, child.partition, child.offset, child.fetchSize, child.leaderEpoch)
		}
	}

	// avoid to fetch when there is no block
	if len(request.blocks) == 0 {
		return nil, nil
	}

	return bc.broker.Fetch(request)
}
