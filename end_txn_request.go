package sarama

type EndTxnRequest struct {
	Version           int16
	TransactionalID   string
	ProducerID        int64
	ProducerEpoch     int16
	TransactionResult bool
}

func (a *EndTxnRequest) setVersion(v int16) {
	a.Version = v
}

func (a *EndTxnRequest) encode(pe packetEncoder) error {
	if err := pe.putString(a.TransactionalID); err != nil {
		return err
	}

	pe.putInt64(a.ProducerID)

	pe.putInt16(a.ProducerEpoch)

	pe.putBool(a.TransactionResult)

	return nil
}

func (a *EndTxnRequest) decode(pd packetDecoder, version int16) (err error) {
	if a.TransactionalID, err = pd.getString(); err != nil {
		return err
	}
	if a.ProducerID, err = pd.getInt64(); err != nil {
		return err
	}
	if a.ProducerEpoch, err = pd.getInt16(); err != nil {
		return err
	}
	if a.TransactionResult, err = pd.getBool(); err != nil {
		return err
	}
	return nil
}

func (a *EndTxnRequest) key() int16 {
	return apiKeyEndTxn
}

func (a *EndTxnRequest) version() int16 {
	return a.Version
}

func (r *EndTxnRequest) headerVersion() int16 {
	return 1
}

func (a *EndTxnRequest) isValidVersion() bool {
	return a.Version >= 0 && a.Version <= 2
}

func (a *EndTxnRequest) requiredVersion() KafkaVersion {
	switch a.Version {
	case 2:
		return V2_7_0_0
	case 1:
		return V2_0_0_0
	default:
		return V0_11_0_0
	}
}
