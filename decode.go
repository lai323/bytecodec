package bytecodec

import "reflect"

// An InvalidUnmarshalError describes an invalid argument passed to Unmarshal.
// (The argument to Unmarshal must be a non-nil pointer.)
type InvalidUnmarshalError struct {
	Type reflect.Type
}

func (e *InvalidUnmarshalError) Error() string {
	if e.Type == nil {
		return "bytecodec: Unmarshal(nil)"
	}

	if e.Type.Kind() != reflect.Ptr {
		return "bytecodec: Unmarshal(non-pointer " + e.Type.String() + ")"
	}
	return "bytecodec: Unmarshal(nil " + e.Type.String() + ")"
}

func Unmarshal(data []byte, v interface{}) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return &InvalidUnmarshalError{reflect.TypeOf(v)}
	}

	d := newCodecState()
	d.Write(data)
	err := d.unmarshal(rv)
	if err != nil {
		return err
	}

	encodeStatePool.Put(d)
	return nil
}
