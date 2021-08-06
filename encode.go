package bytecodec

func Marshal(v interface{}) ([]byte, error) {
	e := newCodecState([]byte{})

	err := e.marshal(v)
	if err != nil {
		return nil, err
	}
	buf := append([]byte(nil), e.Bytes()...)

	encodeStatePool.Put(e)
	return buf, nil
}
