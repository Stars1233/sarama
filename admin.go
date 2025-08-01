package sarama

import (
	"errors"
	"fmt"
	"io"
	"math/rand"
	"strconv"
	"sync"
	"time"
)

// ClusterAdmin is the administrative client for Kafka, which supports managing and inspecting topics,
// brokers, configurations and ACLs. The minimum broker version required is 0.10.0.0.
// Methods with stricter requirements will specify the minimum broker version required.
// You MUST call Close() on a client to avoid leaks
type ClusterAdmin interface {
	// Creates a new topic. This operation is supported by brokers with version 0.10.1.0 or higher.
	// It may take several seconds after CreateTopic returns success for all the brokers
	// to become aware that the topic has been created. During this time, listTopics
	// may not return information about the new topic.The validateOnly option is supported from version 0.10.2.0.
	CreateTopic(topic string, detail *TopicDetail, validateOnly bool) error

	// List the topics available in the cluster with the default options.
	ListTopics() (map[string]TopicDetail, error)

	// Describe some topics in the cluster.
	DescribeTopics(topics []string) (metadata []*TopicMetadata, err error)

	// Delete a topic. It may take several seconds after the DeleteTopic to returns success
	// and for all the brokers to become aware that the topics are gone.
	// During this time, listTopics  may continue to return information about the deleted topic.
	// If delete.topic.enable is false on the brokers, deleteTopic will mark
	// the topic for deletion, but not actually delete them.
	// This operation is supported by brokers with version 0.10.1.0 or higher.
	DeleteTopic(topic string) error

	// Increase the number of partitions of the topics  according to the corresponding values.
	// If partitions are increased for a topic that has a key, the partition logic or ordering of
	// the messages will be affected. It may take several seconds after this method returns
	// success for all the brokers to become aware that the partitions have been created.
	// During this time, ClusterAdmin#describeTopics may not return information about the
	// new partitions. This operation is supported by brokers with version 1.0.0 or higher.
	CreatePartitions(topic string, count int32, assignment [][]int32, validateOnly bool) error

	// Alter the replica assignment for partitions.
	// This operation is supported by brokers with version 2.4.0.0 or higher.
	AlterPartitionReassignments(topic string, assignment [][]int32) error

	// Provides info on ongoing partitions replica reassignments.
	// This operation is supported by brokers with version 2.4.0.0 or higher.
	ListPartitionReassignments(topics string, partitions []int32) (topicStatus map[string]map[int32]*PartitionReplicaReassignmentsStatus, err error)

	// Delete records whose offset is smaller than the given offset of the corresponding partition.
	// This operation is supported by brokers with version 0.11.0.0 or higher.
	DeleteRecords(topic string, partitionOffsets map[int32]int64) error

	// Get the configuration for the specified resources.
	// The returned configuration includes default values and the Default is true
	// can be used to distinguish them from user supplied values.
	// Config entries where ReadOnly is true cannot be updated.
	// The value of config entries where Sensitive is true is always nil so
	// sensitive information is not disclosed.
	// This operation is supported by brokers with version 0.11.0.0 or higher.
	DescribeConfig(resource ConfigResource) ([]ConfigEntry, error)

	// Update the configuration for the specified resources with the default options.
	// This operation is supported by brokers with version 0.11.0.0 or higher.
	// The resources with their configs (topic is the only resource type with configs
	// that can be updated currently Updates are not transactional so they may succeed
	// for some resources while fail for others. The configs for a particular resource are updated automatically.
	AlterConfig(resourceType ConfigResourceType, name string, entries map[string]*string, validateOnly bool) error

	// IncrementalAlterConfig Incrementally Update the configuration for the specified resources with the default options.
	// This operation is supported by brokers with version 2.3.0.0 or higher.
	// Updates are not transactional so they may succeed for some resources while fail for others.
	// The configs for a particular resource are updated automatically.
	IncrementalAlterConfig(resourceType ConfigResourceType, name string, entries map[string]IncrementalAlterConfigsEntry, validateOnly bool) error

	// Creates an access control list (ACL) which is bound to a specific resource.
	// This operation is not transactional so it may succeed or fail.
	// If you attempt to add an ACL that duplicates an existing ACL, no error will be raised, but
	// no changes will be made. This operation is supported by brokers with version 0.11.0.0 or higher.
	// Deprecated: Use CreateACLs instead.
	CreateACL(resource Resource, acl Acl) error

	// Creates access control lists (ACLs) which are bound to specific resources.
	// This operation is not transactional so it may succeed for some ACLs while fail for others.
	// If you attempt to add an ACL that duplicates an existing ACL, no error will be raised, but
	// no changes will be made. This operation is supported by brokers with version 0.11.0.0 or higher.
	CreateACLs([]*ResourceAcls) error

	// Lists access control lists (ACLs) according to the supplied filter.
	// it may take some time for changes made by createAcls or deleteAcls to be reflected in the output of ListAcls
	// This operation is supported by brokers with version 0.11.0.0 or higher.
	ListAcls(filter AclFilter) ([]ResourceAcls, error)

	// Deletes access control lists (ACLs) according to the supplied filters.
	// This operation is not transactional so it may succeed for some ACLs while fail for others.
	// This operation is supported by brokers with version 0.11.0.0 or higher.
	DeleteACL(filter AclFilter, validateOnly bool) ([]MatchingAcl, error)

	// ElectLeaders allows to trigger the election of preferred leaders for a set of partitions.
	ElectLeaders(ElectionType, map[string][]int32) (map[string]map[int32]*PartitionResult, error)

	// List the consumer groups available in the cluster.
	ListConsumerGroups() (map[string]string, error)

	// Describe the given consumer groups.
	DescribeConsumerGroups(groups []string) ([]*GroupDescription, error)

	// List the consumer group offsets available in the cluster.
	ListConsumerGroupOffsets(group string, topicPartitions map[string][]int32) (*OffsetFetchResponse, error)

	// Deletes a consumer group offset
	DeleteConsumerGroupOffset(group string, topic string, partition int32) error

	// Delete a consumer group.
	DeleteConsumerGroup(group string) error

	// Get information about the nodes in the cluster
	DescribeCluster() (brokers []*Broker, controllerID int32, err error)

	// Get information about all log directories on the given set of brokers
	DescribeLogDirs(brokers []int32) (map[int32][]DescribeLogDirsResponseDirMetadata, error)

	// Get information about SCRAM users
	DescribeUserScramCredentials(users []string) ([]*DescribeUserScramCredentialsResult, error)

	// Delete SCRAM users
	DeleteUserScramCredentials(delete []AlterUserScramCredentialsDelete) ([]*AlterUserScramCredentialsResult, error)

	// Upsert SCRAM users
	UpsertUserScramCredentials(upsert []AlterUserScramCredentialsUpsert) ([]*AlterUserScramCredentialsResult, error)

	// Get client quota configurations corresponding to the specified filter.
	// This operation is supported by brokers with version 2.6.0.0 or higher.
	DescribeClientQuotas(components []QuotaFilterComponent, strict bool) ([]DescribeClientQuotasEntry, error)

	// Alters client quota configurations with the specified alterations.
	// This operation is supported by brokers with version 2.6.0.0 or higher.
	AlterClientQuotas(entity []QuotaEntityComponent, op ClientQuotasOp, validateOnly bool) error

	// Controller returns the cluster controller broker. It will return a
	// locally cached value if it's available.
	Controller() (*Broker, error)

	// Coordinator returns the coordinating broker for a consumer group. It will
	// return a locally cached value if it's available.
	Coordinator(group string) (*Broker, error)

	// Remove members from the consumer group by given member identities.
	// This operation is supported by brokers with version 2.3 or higher
	// This is for static membership feature. KIP-345
	RemoveMemberFromConsumerGroup(groupId string, groupInstanceIds []string) (*LeaveGroupResponse, error)

	// Close shuts down the admin and closes underlying client.
	Close() error
}

type clusterAdmin struct {
	client Client
	conf   *Config
}

// NewClusterAdmin creates a new ClusterAdmin using the given broker addresses and configuration.
func NewClusterAdmin(addrs []string, conf *Config) (ClusterAdmin, error) {
	client, err := NewClient(addrs, conf)
	if err != nil {
		return nil, err
	}
	admin, err := NewClusterAdminFromClient(client)
	if err != nil {
		client.Close()
	}
	return admin, err
}

// NewClusterAdminFromClient creates a new ClusterAdmin using the given client.
// Note that underlying client will also be closed on admin's Close() call.
func NewClusterAdminFromClient(client Client) (ClusterAdmin, error) {
	// make sure we can retrieve the controller
	_, err := client.Controller()
	if err != nil {
		return nil, err
	}

	ca := &clusterAdmin{
		client: client,
		conf:   client.Config(),
	}
	return ca, nil
}

func (ca *clusterAdmin) Close() error {
	return ca.client.Close()
}

func (ca *clusterAdmin) Controller() (*Broker, error) {
	return ca.client.Controller()
}

func (ca *clusterAdmin) Coordinator(group string) (*Broker, error) {
	return ca.client.Coordinator(group)
}

func (ca *clusterAdmin) refreshController() (*Broker, error) {
	return ca.client.RefreshController()
}

// isRetriableControllerError returns `true` if the given error type unwraps to
// an `ErrNotController` or `EOF` response from Kafka
func isRetriableControllerError(err error) bool {
	return errors.Is(err, ErrNotController) || errors.Is(err, io.EOF)
}

// isRetriableGroupCoordinatorError returns `true` if the given error type
// unwraps to an `ErrNotCoordinatorForConsumer`,
// `ErrConsumerCoordinatorNotAvailable` or `EOF` response from Kafka
func isRetriableGroupCoordinatorError(err error) bool {
	return errors.Is(err, ErrNotCoordinatorForConsumer) || errors.Is(err, ErrConsumerCoordinatorNotAvailable) || errors.Is(err, io.EOF)
}

// retryOnError will repeatedly call the given (error-returning) func in the
// case that its response is non-nil and retryable (as determined by the
// provided retryable func) up to the maximum number of tries permitted by
// the admin client configuration
func (ca *clusterAdmin) retryOnError(retryable func(error) bool, fn func() error) error {
	for attemptsRemaining := ca.conf.Admin.Retry.Max + 1; ; {
		err := fn()
		attemptsRemaining--
		if err == nil || attemptsRemaining <= 0 || !retryable(err) {
			return err
		}
		Logger.Printf(
			"admin/request retrying after %dms... (%d attempts remaining)\n",
			ca.conf.Admin.Retry.Backoff/time.Millisecond, attemptsRemaining)
		time.Sleep(ca.conf.Admin.Retry.Backoff)
	}
}

func (ca *clusterAdmin) CreateTopic(topic string, detail *TopicDetail, validateOnly bool) error {
	if topic == "" {
		return ErrInvalidTopic
	}

	if detail == nil {
		return errors.New("you must specify topic details")
	}

	topicDetails := make(map[string]*TopicDetail)
	topicDetails[topic] = detail

	request := &CreateTopicsRequest{
		TopicDetails: topicDetails,
		ValidateOnly: validateOnly,
		Timeout:      ca.conf.Admin.Timeout,
	}

	if ca.conf.Version.IsAtLeast(V2_0_0_0) {
		// Version 3 is the same as version 2 (brokers response before throttling)
		request.Version = 3
	} else if ca.conf.Version.IsAtLeast(V0_11_0_0) {
		// Version 2 is the same as version 1 (response has ThrottleTime)
		request.Version = 2
	} else if ca.conf.Version.IsAtLeast(V0_10_2_0) {
		// Version 1 adds validateOnly.
		request.Version = 1
	}

	return ca.retryOnError(isRetriableControllerError, func() error {
		b, err := ca.Controller()
		if err != nil {
			return err
		}

		rsp, err := b.CreateTopics(request)
		if err != nil {
			return err
		}

		topicErr, ok := rsp.TopicErrors[topic]
		if !ok {
			return ErrIncompleteResponse
		}

		if !errors.Is(topicErr.Err, ErrNoError) {
			if isRetriableControllerError(topicErr.Err) {
				_, _ = ca.refreshController()
			}
			return topicErr
		}

		return nil
	})
}

func (ca *clusterAdmin) DescribeTopics(topics []string) (metadata []*TopicMetadata, err error) {
	var response *MetadataResponse
	err = ca.retryOnError(isRetriableControllerError, func() error {
		controller, err := ca.Controller()
		if err != nil {
			return err
		}
		request := NewMetadataRequest(ca.conf.Version, topics)
		response, err = controller.GetMetadata(request)
		if isRetriableControllerError(err) {
			_, _ = ca.refreshController()
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return response.Topics, nil
}

func (ca *clusterAdmin) DescribeCluster() (brokers []*Broker, controllerID int32, err error) {
	var response *MetadataResponse
	err = ca.retryOnError(isRetriableControllerError, func() error {
		controller, err := ca.Controller()
		if err != nil {
			return err
		}

		request := NewMetadataRequest(ca.conf.Version, nil)
		response, err = controller.GetMetadata(request)
		if isRetriableControllerError(err) {
			_, _ = ca.refreshController()
		}
		return err
	})
	if err != nil {
		return nil, int32(0), err
	}

	return response.Brokers, response.ControllerID, nil
}

func (ca *clusterAdmin) findBroker(id int32) (*Broker, error) {
	brokers := ca.client.Brokers()
	for _, b := range brokers {
		if b.ID() == id {
			return b, nil
		}
	}
	return nil, fmt.Errorf("could not find broker id %d", id)
}

func (ca *clusterAdmin) findAnyBroker() (*Broker, error) {
	brokers := ca.client.Brokers()
	if len(brokers) > 0 {
		index := rand.Intn(len(brokers))
		return brokers[index], nil
	}
	return nil, errors.New("no available broker")
}

func (ca *clusterAdmin) ListTopics() (map[string]TopicDetail, error) {
	// In order to build TopicDetails we need to first get the list of all
	// topics using a MetadataRequest and then get their configs using a
	// DescribeConfigsRequest request. To avoid sending many requests to the
	// broker, we use a single DescribeConfigsRequest.

	// Send the all-topic MetadataRequest
	b, err := ca.findAnyBroker()
	if err != nil {
		return nil, err
	}
	_ = b.Open(ca.client.Config())

	metadataReq := NewMetadataRequest(ca.conf.Version, nil)
	metadataResp, err := b.GetMetadata(metadataReq)
	if err != nil {
		return nil, err
	}

	topicsDetailsMap := make(map[string]TopicDetail)

	var describeConfigsResources []*ConfigResource

	for _, topic := range metadataResp.Topics {
		topicDetails := TopicDetail{
			NumPartitions: int32(len(topic.Partitions)),
		}
		if len(topic.Partitions) > 0 {
			topicDetails.ReplicaAssignment = map[int32][]int32{}
			for _, partition := range topic.Partitions {
				topicDetails.ReplicaAssignment[partition.ID] = partition.Replicas
			}
			topicDetails.ReplicationFactor = int16(len(topic.Partitions[0].Replicas))
		}
		topicsDetailsMap[topic.Name] = topicDetails

		// we populate the resources we want to describe from the MetadataResponse
		topicResource := ConfigResource{
			Type: TopicResource,
			Name: topic.Name,
		}
		describeConfigsResources = append(describeConfigsResources, &topicResource)
	}

	// Send the DescribeConfigsRequest
	describeConfigsReq := &DescribeConfigsRequest{
		Resources: describeConfigsResources,
	}

	if ca.conf.Version.IsAtLeast(V1_1_0_0) {
		describeConfigsReq.Version = 1
	}

	if ca.conf.Version.IsAtLeast(V2_0_0_0) {
		describeConfigsReq.Version = 2
	}

	describeConfigsResp, err := b.DescribeConfigs(describeConfigsReq)
	if err != nil {
		return nil, err
	}

	for _, resource := range describeConfigsResp.Resources {
		topicDetails := topicsDetailsMap[resource.Name]
		topicDetails.ConfigEntries = make(map[string]*string)

		for _, entry := range resource.Configs {
			// only include non-default non-sensitive config
			// (don't actually think topic config will ever be sensitive)
			if entry.Default || entry.Sensitive {
				continue
			}
			topicDetails.ConfigEntries[entry.Name] = &entry.Value
		}

		topicsDetailsMap[resource.Name] = topicDetails
	}

	return topicsDetailsMap, nil
}

func (ca *clusterAdmin) DeleteTopic(topic string) error {
	if topic == "" {
		return ErrInvalidTopic
	}

	request := &DeleteTopicsRequest{
		Topics:  []string{topic},
		Timeout: ca.conf.Admin.Timeout,
	}

	// Versions 0, 1, 2, and 3 are the same.
	if ca.conf.Version.IsAtLeast(V2_1_0_0) {
		request.Version = 3
	} else if ca.conf.Version.IsAtLeast(V2_0_0_0) {
		request.Version = 2
	} else if ca.conf.Version.IsAtLeast(V0_11_0_0) {
		request.Version = 1
	}

	return ca.retryOnError(isRetriableControllerError, func() error {
		b, err := ca.Controller()
		if err != nil {
			return err
		}

		rsp, err := b.DeleteTopics(request)
		if err != nil {
			return err
		}

		topicErr, ok := rsp.TopicErrorCodes[topic]
		if !ok {
			return ErrIncompleteResponse
		}

		if !errors.Is(topicErr, ErrNoError) {
			if errors.Is(topicErr, ErrNotController) {
				_, _ = ca.refreshController()
			}
			return topicErr
		}

		return nil
	})
}

func (ca *clusterAdmin) CreatePartitions(topic string, count int32, assignment [][]int32, validateOnly bool) error {
	if topic == "" {
		return ErrInvalidTopic
	}

	topicPartitions := make(map[string]*TopicPartition)
	topicPartitions[topic] = &TopicPartition{Count: count, Assignment: assignment}

	request := &CreatePartitionsRequest{
		TopicPartitions: topicPartitions,
		Timeout:         ca.conf.Admin.Timeout,
		ValidateOnly:    validateOnly,
	}
	if ca.conf.Version.IsAtLeast(V2_0_0_0) {
		request.Version = 1
	}

	return ca.retryOnError(isRetriableControllerError, func() error {
		b, err := ca.Controller()
		if err != nil {
			return err
		}

		rsp, err := b.CreatePartitions(request)
		if err != nil {
			return err
		}

		topicErr, ok := rsp.TopicPartitionErrors[topic]
		if !ok {
			return ErrIncompleteResponse
		}

		if !errors.Is(topicErr.Err, ErrNoError) {
			if errors.Is(topicErr.Err, ErrNotController) {
				_, _ = ca.refreshController()
			}
			return topicErr
		}

		return nil
	})
}

func (ca *clusterAdmin) AlterPartitionReassignments(topic string, assignment [][]int32) error {
	if topic == "" {
		return ErrInvalidTopic
	}

	request := &AlterPartitionReassignmentsRequest{
		TimeoutMs: int32(60000),
		Version:   int16(0),
	}

	for i := 0; i < len(assignment); i++ {
		request.AddBlock(topic, int32(i), assignment[i])
	}

	return ca.retryOnError(isRetriableControllerError, func() error {
		b, err := ca.Controller()
		if err != nil {
			return err
		}

		errs := make([]error, 0)

		rsp, err := b.AlterPartitionReassignments(request)

		if err != nil {
			errs = append(errs, err)
		} else {
			if rsp.ErrorCode > 0 {
				errs = append(errs, rsp.ErrorCode)
			}

			for topic, topicErrors := range rsp.Errors {
				for partition, partitionError := range topicErrors {
					if !errors.Is(partitionError.errorCode, ErrNoError) {
						errs = append(errs, fmt.Errorf("[%s-%d]: %w", topic, partition, partitionError.errorCode))
					}
				}
			}
		}

		if len(errs) > 0 {
			return Wrap(ErrReassignPartitions, errs...)
		}

		return nil
	})
}

func (ca *clusterAdmin) ListPartitionReassignments(topic string, partitions []int32) (topicStatus map[string]map[int32]*PartitionReplicaReassignmentsStatus, err error) {
	if topic == "" {
		return nil, ErrInvalidTopic
	}

	request := &ListPartitionReassignmentsRequest{
		TimeoutMs: int32(60000),
		Version:   int16(0),
	}

	request.AddBlock(topic, partitions)

	var rsp *ListPartitionReassignmentsResponse
	err = ca.retryOnError(isRetriableControllerError, func() error {
		b, err := ca.Controller()
		if err != nil {
			return err
		}
		_ = b.Open(ca.client.Config())

		rsp, err = b.ListPartitionReassignments(request)
		if isRetriableControllerError(err) {
			_, _ = ca.refreshController()
		}
		return err
	})

	if err == nil && rsp != nil {
		return rsp.TopicStatus, nil
	} else {
		return nil, err
	}
}

func (ca *clusterAdmin) DeleteRecords(topic string, partitionOffsets map[int32]int64) error {
	if topic == "" {
		return ErrInvalidTopic
	}
	errs := make([]error, 0)
	partitionPerBroker := make(map[*Broker][]int32)
	for partition := range partitionOffsets {
		broker, err := ca.client.Leader(topic, partition)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		partitionPerBroker[broker] = append(partitionPerBroker[broker], partition)
	}
	for broker, partitions := range partitionPerBroker {
		topics := make(map[string]*DeleteRecordsRequestTopic)
		recordsToDelete := make(map[int32]int64)
		for _, p := range partitions {
			recordsToDelete[p] = partitionOffsets[p]
		}
		topics[topic] = &DeleteRecordsRequestTopic{
			PartitionOffsets: recordsToDelete,
		}
		request := &DeleteRecordsRequest{
			Topics:  topics,
			Timeout: ca.conf.Admin.Timeout,
		}
		if ca.conf.Version.IsAtLeast(V2_0_0_0) {
			request.Version = 1
		}
		rsp, err := broker.DeleteRecords(request)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		deleteRecordsResponseTopic, ok := rsp.Topics[topic]
		if !ok {
			errs = append(errs, ErrIncompleteResponse)
			continue
		}

		for _, deleteRecordsResponsePartition := range deleteRecordsResponseTopic.Partitions {
			if !errors.Is(deleteRecordsResponsePartition.Err, ErrNoError) {
				errs = append(errs, deleteRecordsResponsePartition.Err)
				continue
			}
		}
	}
	if len(errs) > 0 {
		return Wrap(ErrDeleteRecords, errs...)
	}
	// todo since we are dealing with couple of partitions it would be good if we return slice of errors
	// for each partition instead of one error
	return nil
}

// Returns a bool indicating whether the resource request needs to go to a
// specific broker
func dependsOnSpecificNode(resource ConfigResource) bool {
	return (resource.Type == BrokerResource && resource.Name != "") ||
		resource.Type == BrokerLoggerResource
}

func (ca *clusterAdmin) DescribeConfig(resource ConfigResource) ([]ConfigEntry, error) {
	var entries []ConfigEntry
	var resources []*ConfigResource
	resources = append(resources, &resource)

	request := &DescribeConfigsRequest{
		Resources: resources,
	}

	if ca.conf.Version.IsAtLeast(V1_1_0_0) {
		request.Version = 1
	}

	if ca.conf.Version.IsAtLeast(V2_0_0_0) {
		request.Version = 2
	}

	var (
		b   *Broker
		err error
	)

	// DescribeConfig of broker/broker logger must be sent to the broker in question
	if dependsOnSpecificNode(resource) {
		var id int64
		id, err = strconv.ParseInt(resource.Name, 10, 32)
		if err != nil {
			return nil, err
		}
		b, err = ca.findBroker(int32(id))
	} else {
		b, err = ca.findAnyBroker()
	}
	if err != nil {
		return nil, err
	}

	_ = b.Open(ca.client.Config())
	rsp, err := b.DescribeConfigs(request)
	if err != nil {
		return nil, err
	}

	for _, rspResource := range rsp.Resources {
		if rspResource.Name == resource.Name {
			if rspResource.ErrorCode != 0 {
				return nil, &DescribeConfigError{Err: KError(rspResource.ErrorCode), ErrMsg: rspResource.ErrorMsg}
			}
			for _, cfgEntry := range rspResource.Configs {
				entries = append(entries, *cfgEntry)
			}
		}
	}
	return entries, nil
}

func (ca *clusterAdmin) AlterConfig(resourceType ConfigResourceType, name string, entries map[string]*string, validateOnly bool) error {
	var resources []*AlterConfigsResource
	resources = append(resources, &AlterConfigsResource{
		Type:          resourceType,
		Name:          name,
		ConfigEntries: entries,
	})

	request := &AlterConfigsRequest{
		Resources:    resources,
		ValidateOnly: validateOnly,
	}
	if ca.conf.Version.IsAtLeast(V2_0_0_0) {
		request.Version = 1
	}

	var (
		b   *Broker
		err error
	)

	// AlterConfig of broker/broker logger must be sent to the broker in question
	if dependsOnSpecificNode(ConfigResource{Name: name, Type: resourceType}) {
		var id int64
		id, err = strconv.ParseInt(name, 10, 32)
		if err != nil {
			return err
		}
		b, err = ca.findBroker(int32(id))
	} else {
		b, err = ca.findAnyBroker()
	}
	if err != nil {
		return err
	}

	_ = b.Open(ca.client.Config())
	rsp, err := b.AlterConfigs(request)
	if err != nil {
		return err
	}

	for _, rspResource := range rsp.Resources {
		if rspResource.Name == name {
			if rspResource.ErrorCode != 0 {
				return &AlterConfigError{Err: KError(rspResource.ErrorCode), ErrMsg: rspResource.ErrorMsg}
			}
		}
	}
	return nil
}

func (ca *clusterAdmin) IncrementalAlterConfig(resourceType ConfigResourceType, name string, entries map[string]IncrementalAlterConfigsEntry, validateOnly bool) error {
	var resources []*IncrementalAlterConfigsResource
	resources = append(resources, &IncrementalAlterConfigsResource{
		Type:          resourceType,
		Name:          name,
		ConfigEntries: entries,
	})

	request := &IncrementalAlterConfigsRequest{
		Resources:    resources,
		ValidateOnly: validateOnly,
	}

	var (
		b   *Broker
		err error
	)

	// AlterConfig of broker/broker logger must be sent to the broker in question
	if dependsOnSpecificNode(ConfigResource{Name: name, Type: resourceType}) {
		var id int64
		id, err = strconv.ParseInt(name, 10, 32)
		if err != nil {
			return err
		}
		b, err = ca.findBroker(int32(id))
	} else {
		b, err = ca.findAnyBroker()
	}
	if err != nil {
		return err
	}

	_ = b.Open(ca.client.Config())
	rsp, err := b.IncrementalAlterConfigs(request)
	if err != nil {
		return err
	}

	for _, rspResource := range rsp.Resources {
		if rspResource.Name == name {
			if rspResource.ErrorMsg != "" {
				return errors.New(rspResource.ErrorMsg)
			}
			if rspResource.ErrorCode != 0 {
				return KError(rspResource.ErrorCode)
			}
		}
	}
	return nil
}

func (ca *clusterAdmin) CreateACL(resource Resource, acl Acl) error {
	var acls []*AclCreation
	acls = append(acls, &AclCreation{resource, acl})
	request := &CreateAclsRequest{AclCreations: acls}

	if ca.conf.Version.IsAtLeast(V2_0_0_0) {
		request.Version = 1
	}

	b, err := ca.Controller()
	if err != nil {
		return err
	}

	_, err = b.CreateAcls(request)
	return err
}

func (ca *clusterAdmin) CreateACLs(resourceACLs []*ResourceAcls) error {
	var acls []*AclCreation
	for _, resourceACL := range resourceACLs {
		for _, acl := range resourceACL.Acls {
			acls = append(acls, &AclCreation{resourceACL.Resource, *acl})
		}
	}
	request := &CreateAclsRequest{AclCreations: acls}

	if ca.conf.Version.IsAtLeast(V2_0_0_0) {
		request.Version = 1
	}

	b, err := ca.Controller()
	if err != nil {
		return err
	}

	_, err = b.CreateAcls(request)
	return err
}

func (ca *clusterAdmin) ListAcls(filter AclFilter) ([]ResourceAcls, error) {
	request := &DescribeAclsRequest{AclFilter: filter}

	if ca.conf.Version.IsAtLeast(V2_0_0_0) {
		request.Version = 1
	}

	b, err := ca.Controller()
	if err != nil {
		return nil, err
	}

	rsp, err := b.DescribeAcls(request)
	if err != nil {
		return nil, err
	}

	var lAcls []ResourceAcls
	for _, rAcl := range rsp.ResourceAcls {
		lAcls = append(lAcls, *rAcl)
	}
	return lAcls, nil
}

func (ca *clusterAdmin) DeleteACL(filter AclFilter, validateOnly bool) ([]MatchingAcl, error) {
	var filters []*AclFilter
	filters = append(filters, &filter)
	request := &DeleteAclsRequest{Filters: filters}

	if ca.conf.Version.IsAtLeast(V2_0_0_0) {
		request.Version = 1
	}

	b, err := ca.Controller()
	if err != nil {
		return nil, err
	}

	rsp, err := b.DeleteAcls(request)
	if err != nil {
		return nil, err
	}

	var mAcls []MatchingAcl
	for _, fr := range rsp.FilterResponses {
		for _, mACL := range fr.MatchingAcls {
			mAcls = append(mAcls, *mACL)
		}
	}
	return mAcls, nil
}

func (ca *clusterAdmin) ElectLeaders(electionType ElectionType, partitions map[string][]int32) (map[string]map[int32]*PartitionResult, error) {
	request := &ElectLeadersRequest{
		Type:            electionType,
		TopicPartitions: partitions,
		TimeoutMs:       int32(60000),
	}

	if ca.conf.Version.IsAtLeast(V2_4_0_0) {
		request.Version = 2
	} else if ca.conf.Version.IsAtLeast(V0_11_0_0) {
		request.Version = 1
	}

	var res *ElectLeadersResponse
	if err := ca.retryOnError(isRetriableControllerError, func() error {
		b, err := ca.Controller()
		if err != nil {
			return err
		}
		_ = b.Open(ca.client.Config())

		res, err = b.ElectLeaders(request)
		if err != nil {
			return err
		}
		if !errors.Is(res.ErrorCode, ErrNoError) {
			if isRetriableControllerError(res.ErrorCode) {
				_, _ = ca.refreshController()
			}
			return res.ErrorCode
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return res.ReplicaElectionResults, nil
}

func (ca *clusterAdmin) DescribeConsumerGroups(groups []string) (result []*GroupDescription, err error) {
	groupsPerBroker := make(map[*Broker][]string)

	for _, group := range groups {
		coordinator, err := ca.client.Coordinator(group)
		if err != nil {
			return nil, err
		}
		groupsPerBroker[coordinator] = append(groupsPerBroker[coordinator], group)
	}

	for broker, brokerGroups := range groupsPerBroker {
		describeReq := &DescribeGroupsRequest{
			Groups: brokerGroups,
		}

		if ca.conf.Version.IsAtLeast(V2_4_0_0) {
			// Starting in version 4, the response will include group.instance.id info for members.
			describeReq.Version = 4
		} else if ca.conf.Version.IsAtLeast(V2_3_0_0) {
			// Starting in version 3, authorized operations can be requested.
			describeReq.Version = 3
		} else if ca.conf.Version.IsAtLeast(V2_0_0_0) {
			// Version 2 is the same as version 0.
			describeReq.Version = 2
		} else if ca.conf.Version.IsAtLeast(V1_1_0_0) {
			// Version 1 is the same as version 0.
			describeReq.Version = 1
		}
		response, err := broker.DescribeGroups(describeReq)
		if err != nil {
			return nil, err
		}

		result = append(result, response.Groups...)
	}
	return result, nil
}

func (ca *clusterAdmin) ListConsumerGroups() (allGroups map[string]string, err error) {
	allGroups = make(map[string]string)

	// Query brokers in parallel, since we have to query *all* brokers
	brokers := ca.client.Brokers()
	groupMaps := make(chan map[string]string, len(brokers))
	errChan := make(chan error, len(brokers))
	wg := sync.WaitGroup{}

	for _, b := range brokers {
		wg.Add(1)
		go func(b *Broker, conf *Config) {
			defer wg.Done()
			_ = b.Open(conf) // Ensure that broker is opened

			request := &ListGroupsRequest{}
			if ca.conf.Version.IsAtLeast(V2_6_0_0) {
				// Version 4 adds the StatesFilter field (KIP-518).
				request.Version = 4
			} else if ca.conf.Version.IsAtLeast(V2_4_0_0) {
				// Version 3 is the first flexible version.
				request.Version = 3
			} else if ca.conf.Version.IsAtLeast(V2_0_0_0) {
				// Version 2 is the same as version 0.
				request.Version = 2
			} else if ca.conf.Version.IsAtLeast(V0_11_0_0) {
				// Version 1 is the same as version 0.
				request.Version = 1
			}

			response, err := b.ListGroups(request)
			if err != nil {
				errChan <- err
				return
			}

			groups := make(map[string]string)
			for group, typ := range response.Groups {
				groups[group] = typ
			}

			groupMaps <- groups
		}(b, ca.conf)
	}

	wg.Wait()
	close(groupMaps)
	close(errChan)

	for groupMap := range groupMaps {
		for group, protocolType := range groupMap {
			allGroups[group] = protocolType
		}
	}

	// Intentionally return only the first error for simplicity
	err = <-errChan
	return
}

func (ca *clusterAdmin) ListConsumerGroupOffsets(group string, topicPartitions map[string][]int32) (*OffsetFetchResponse, error) {
	var response *OffsetFetchResponse
	request := NewOffsetFetchRequest(ca.conf.Version, group, topicPartitions)
	err := ca.retryOnError(isRetriableGroupCoordinatorError, func() (err error) {
		defer func() {
			if err != nil && isRetriableGroupCoordinatorError(err) {
				_ = ca.client.RefreshCoordinator(group)
			}
		}()

		coordinator, err := ca.client.Coordinator(group)
		if err != nil {
			return err
		}

		response, err = coordinator.FetchOffset(request)
		if err != nil {
			return err
		}
		if !errors.Is(response.Err, ErrNoError) {
			return response.Err
		}

		return nil
	})

	return response, err
}

func (ca *clusterAdmin) DeleteConsumerGroupOffset(group string, topic string, partition int32) error {
	var response *DeleteOffsetsResponse
	request := &DeleteOffsetsRequest{
		Group: group,
		partitions: map[string][]int32{
			topic: {partition},
		},
	}

	return ca.retryOnError(isRetriableGroupCoordinatorError, func() (err error) {
		defer func() {
			if err != nil && isRetriableGroupCoordinatorError(err) {
				_ = ca.client.RefreshCoordinator(group)
			}
		}()

		coordinator, err := ca.client.Coordinator(group)
		if err != nil {
			return err
		}

		response, err = coordinator.DeleteOffsets(request)
		if err != nil {
			return err
		}
		if !errors.Is(response.ErrorCode, ErrNoError) {
			return response.ErrorCode
		}
		if !errors.Is(response.Errors[topic][partition], ErrNoError) {
			return response.Errors[topic][partition]
		}

		return nil
	})
}

func (ca *clusterAdmin) DeleteConsumerGroup(group string) error {
	var response *DeleteGroupsResponse
	request := &DeleteGroupsRequest{
		Groups: []string{group},
	}
	if ca.conf.Version.IsAtLeast(V2_0_0_0) {
		request.Version = 1
	}

	return ca.retryOnError(isRetriableGroupCoordinatorError, func() (err error) {
		defer func() {
			if err != nil && isRetriableGroupCoordinatorError(err) {
				_ = ca.client.RefreshCoordinator(group)
			}
		}()

		coordinator, err := ca.client.Coordinator(group)
		if err != nil {
			return err
		}

		response, err = coordinator.DeleteGroups(request)
		if err != nil {
			return err
		}

		groupErr, ok := response.GroupErrorCodes[group]
		if !ok {
			return ErrIncompleteResponse
		}

		if !errors.Is(groupErr, ErrNoError) {
			return groupErr
		}

		return nil
	})
}

func (ca *clusterAdmin) DescribeLogDirs(brokerIds []int32) (allLogDirs map[int32][]DescribeLogDirsResponseDirMetadata, err error) {
	allLogDirs = make(map[int32][]DescribeLogDirsResponseDirMetadata)

	// Query brokers in parallel, since we may have to query multiple brokers
	logDirsMaps := make(chan map[int32][]DescribeLogDirsResponseDirMetadata, len(brokerIds))
	errChan := make(chan error, len(brokerIds))
	wg := sync.WaitGroup{}

	for _, b := range brokerIds {
		broker, err := ca.findBroker(b)
		if err != nil {
			Logger.Printf("Unable to find broker with ID = %v\n", b)
			continue
		}
		wg.Add(1)
		go func(b *Broker, conf *Config) {
			defer wg.Done()
			_ = b.Open(conf) // Ensure that broker is opened

			request := &DescribeLogDirsRequest{}
			if ca.conf.Version.IsAtLeast(V2_0_0_0) {
				request.Version = 1
			}
			response, err := b.DescribeLogDirs(request)
			if err != nil {
				errChan <- err
				return
			}
			logDirs := make(map[int32][]DescribeLogDirsResponseDirMetadata)
			logDirs[b.ID()] = response.LogDirs
			logDirsMaps <- logDirs
		}(broker, ca.conf)
	}

	wg.Wait()
	close(logDirsMaps)
	close(errChan)

	for logDirsMap := range logDirsMaps {
		for id, logDirs := range logDirsMap {
			allLogDirs[id] = logDirs
		}
	}

	// Intentionally return only the first error for simplicity
	err = <-errChan
	return
}

func (ca *clusterAdmin) DescribeUserScramCredentials(users []string) ([]*DescribeUserScramCredentialsResult, error) {
	req := &DescribeUserScramCredentialsRequest{}
	for _, u := range users {
		req.DescribeUsers = append(req.DescribeUsers, DescribeUserScramCredentialsRequestUser{
			Name: u,
		})
	}

	b, err := ca.Controller()
	if err != nil {
		return nil, err
	}

	rsp, err := b.DescribeUserScramCredentials(req)
	if err != nil {
		return nil, err
	}

	return rsp.Results, nil
}

func (ca *clusterAdmin) UpsertUserScramCredentials(upsert []AlterUserScramCredentialsUpsert) ([]*AlterUserScramCredentialsResult, error) {
	res, err := ca.AlterUserScramCredentials(upsert, nil)
	if err != nil {
		return nil, err
	}

	return res, nil
}

func (ca *clusterAdmin) DeleteUserScramCredentials(delete []AlterUserScramCredentialsDelete) ([]*AlterUserScramCredentialsResult, error) {
	res, err := ca.AlterUserScramCredentials(nil, delete)
	if err != nil {
		return nil, err
	}

	return res, nil
}

func (ca *clusterAdmin) AlterUserScramCredentials(u []AlterUserScramCredentialsUpsert, d []AlterUserScramCredentialsDelete) ([]*AlterUserScramCredentialsResult, error) {
	req := &AlterUserScramCredentialsRequest{
		Deletions:  d,
		Upsertions: u,
	}

	var rsp *AlterUserScramCredentialsResponse
	err := ca.retryOnError(isRetriableControllerError, func() error {
		b, err := ca.Controller()
		if err != nil {
			return err
		}

		rsp, err = b.AlterUserScramCredentials(req)
		return err
	})
	if err != nil {
		return nil, err
	}

	return rsp.Results, nil
}

// Describe All : use an empty/nil components slice + strict = false
// Contains components: strict = false
// Contains only components: strict = true
func (ca *clusterAdmin) DescribeClientQuotas(components []QuotaFilterComponent, strict bool) ([]DescribeClientQuotasEntry, error) {
	request := &DescribeClientQuotasRequest{
		Components: components,
		Strict:     strict,
	}

	b, err := ca.Controller()
	if err != nil {
		return nil, err
	}

	rsp, err := b.DescribeClientQuotas(request)
	if err != nil {
		return nil, err
	}

	if rsp.ErrorMsg != nil && len(*rsp.ErrorMsg) > 0 {
		return nil, errors.New(*rsp.ErrorMsg)
	}
	if !errors.Is(rsp.ErrorCode, ErrNoError) {
		return nil, rsp.ErrorCode
	}

	return rsp.Entries, nil
}

func (ca *clusterAdmin) AlterClientQuotas(entity []QuotaEntityComponent, op ClientQuotasOp, validateOnly bool) error {
	entry := AlterClientQuotasEntry{
		Entity: entity,
		Ops:    []ClientQuotasOp{op},
	}

	request := &AlterClientQuotasRequest{
		Entries:      []AlterClientQuotasEntry{entry},
		ValidateOnly: validateOnly,
	}

	b, err := ca.Controller()
	if err != nil {
		return err
	}

	rsp, err := b.AlterClientQuotas(request)
	if err != nil {
		return err
	}

	for _, entry := range rsp.Entries {
		if entry.ErrorMsg != nil && len(*entry.ErrorMsg) > 0 {
			return errors.New(*entry.ErrorMsg)
		}
		if !errors.Is(entry.ErrorCode, ErrNoError) {
			return entry.ErrorCode
		}
	}

	return nil
}

func (ca *clusterAdmin) RemoveMemberFromConsumerGroup(group string, groupInstanceIds []string) (*LeaveGroupResponse, error) {
	if !ca.conf.Version.IsAtLeast(V2_4_0_0) {
		return nil, ConfigurationError("Removing members from a consumer group headers requires Kafka version of at least v2.4.0")
	}
	var response *LeaveGroupResponse
	request := &LeaveGroupRequest{
		Version: 3,
		GroupId: group,
	}
	for _, instanceId := range groupInstanceIds {
		groupInstanceId := instanceId
		request.Members = append(request.Members, MemberIdentity{
			GroupInstanceId: &groupInstanceId,
		})
	}
	err := ca.retryOnError(isRetriableGroupCoordinatorError, func() (err error) {
		defer func() {
			if err != nil && isRetriableGroupCoordinatorError(err) {
				_ = ca.client.RefreshCoordinator(group)
			}
		}()

		coordinator, err := ca.client.Coordinator(group)
		if err != nil {
			return err
		}

		response, err = coordinator.LeaveGroup(request)
		if err != nil {
			return err
		}
		if !errors.Is(response.Err, ErrNoError) {
			return response.Err
		}

		return nil
	})

	return response, err
}
