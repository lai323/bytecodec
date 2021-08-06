package bytecodec

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sync"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/simplifiedchinese"
)

type ByteCoder interface {
	MarshalBytes() ([]byte, error)
	UnmarshalBytes(*bytes.Buffer) error
}

// A MarshalerError represents an error from calling a MarshalBytes method.
type MarshalerError struct {
	Type reflect.Type
	Err  error
}

func (e *MarshalerError) Error() string {
	return "json: error calling MarshalBytes" +
		" for type " + e.Type.String() +
		": " + e.Err.Error()
}

// Unwrap returns the underlying error.
func (e *MarshalerError) Unwrap() error { return e.Err }

type UnmarshalerError struct {
	Type reflect.Type
	Err  error
}

func (e *UnmarshalerError) Error() string {
	return "json: error calling UnmarshalBytes" +
		" for type " + e.Type.String() +
		": " + e.Err.Error()
}

func (e *UnmarshalerError) Unwrap() error { return e.Err }

type UnsupportedTypeError struct {
	Type reflect.Type
}

func (e *UnsupportedTypeError) Error() string {
	return "bytecodec: unsupported type: " + e.Type.String()
}

type UnsupportedValueError struct {
	Value reflect.Value
	Str   string
}

func (e *UnsupportedValueError) Error() string {
	return "bytecodec: unsupported value: " + e.Str
}

type codecState struct {
	bytes.Buffer

	// Keep track of what pointers we've seen in the current recursive call
	// path, to avoid cycles that could lead to a stack overflow. Only do
	// the relatively expensive map operations if ptrLevel is larger than
	// startDetectingCyclesAfter, so that we skip the work if we're within a
	// reasonable amount of nested pointers deep.
	ptrLevel uint
	ptrSeen  map[interface{}]struct{}
}

const startDetectingCyclesAfter = 1000

var encodeStatePool sync.Pool

func newCodecState(buf []byte) *codecState {
	if v := encodeStatePool.Get(); v != nil {
		e := v.(*codecState)
		e.Reset()
		if len(e.ptrSeen) > 0 {
			panic("ptrCoder.encode should have emptied ptrSeen via defers")
		}
		e.ptrLevel = 0
		return e
	}
	return &codecState{Buffer: *bytes.NewBuffer(buf), ptrSeen: make(map[interface{}]struct{})}
}

func (c *codecState) marshal(v interface{}) error {
	vv := reflect.ValueOf(v)
	return c.code(valueCodec(vv).encode, vv)
}

func (c *codecState) unmarshal(v reflect.Value) error {
	return c.code(valueCodec(v).decode, v)
}

type bytecodecError struct{ error }

func (c *codecState) code(f func(e *codecState, v reflect.Value), v reflect.Value) (err error) {
	defer func() {
		if r := recover(); r != nil {
			if be, ok := r.(bytecodecError); ok {
				err = be.error
			} else {
				panic(r)
			}
		}
	}()
	f(c, v)
	return nil
}

func (c *codecState) error(err error) {
	panic(bytecodecError{err})
}

var DataLengthErr = errors.New("Not enough data length")

func (c *codecState) ReadByte() byte {
	b, err := c.Buffer.ReadByte()
	if err != nil {
		c.error(bytecodecError{DataLengthErr})
	}
	return b
}

func (c *codecState) Read(p []byte) int {
	n, err := c.Buffer.Read(p)
	if err != nil {
		c.error(bytecodecError{DataLengthErr})
	}
	return n
}

type codec interface {
	encode(e *codecState, v reflect.Value)
	decode(e *codecState, v reflect.Value)
	// encodeBytes(v reflect.Value) ([]byte, error)
	// decodeByLength(e *codecState, v reflect.Value, length int)
}

var codecCache sync.Map // map[reflect.Type]codec

func valueCodec(v reflect.Value) codec {
	if !v.IsValid() {
		return invalidValueCoder{}
	}
	return typeCodec(v.Type())
}

func typeCodec(t reflect.Type) codec {
	if fi, ok := codecCache.Load(t); ok {
		return fi.(codec)
	}
	c := newtypeCodec(t, true)
	codecCache.Store(t, c)
	return c
}

var (
	bytecoderType = reflect.TypeOf((*ByteCoder)(nil)).Elem()
)

func newtypeCodec(t reflect.Type, allowAddr bool) codec {
	if t.Kind() != reflect.Ptr && allowAddr && reflect.PtrTo(t).Implements(bytecoderType) {
		return newCondAddrCoder(addrByteCoderCoder{}, newtypeCodec(t, false))
	}
	if t.Implements(bytecoderType) {
		return byteCoderCoder{}
	}

	switch t.Kind() {
	case reflect.Bool:
		return boolCoder{}
	case reflect.Int8:
		return int8Coder{}
	case reflect.Int16:
		return int16Coder{}
	case reflect.Int32:
		return int32Coder{}
	// Int 被作为 Int64 看待
	case reflect.Int, reflect.Int64:
		return int64Coder{}
	case reflect.Uint8:
		return uint8Coder{}
	case reflect.Uint16:
		return uint16Coder{}
	case reflect.Uint32:
		return uint32Coder{}
	// Uint 被作为 Uint64 看待
	//  Uintptr is a datatype that is large enough to hold a pointer. It is mainly used for unsafe memory access
	case reflect.Uint, reflect.Uint64, reflect.Uintptr:
		return uint64Coder{}
	case reflect.Float32:
		return float32Coder{}
	case reflect.Float64:
		return float64Coder{}
	case reflect.String:
		return stringCoder{}
	case reflect.Interface:
		return interfaceCoder{}
	case reflect.Struct:
		return newStructCoder(t)
	case reflect.Slice, reflect.Array:
		return newArrayCoder(t)
	case reflect.Ptr:
		return newPtrCoder(t)
	default:
		return unsupportedTypeCoder{}
	}
}

type invalidValueCoder struct {
}

func (invalidValueCoder) encode(c *codecState, v reflect.Value) {
}

func (invalidValueCoder) decode(c *codecState, v reflect.Value) {
}

type byteCoderCoder struct{}

func (byteCoderCoder) encode(c *codecState, v reflect.Value) {
	if v.Kind() == reflect.Ptr && v.IsNil() {
		return
	}
	m, ok := v.Interface().(ByteCoder)
	if !ok {
		return
	}
	b, err := m.MarshalBytes()
	if err != nil {
		c.error(&MarshalerError{v.Type(), err})
	}
	c.Write(b)
}

func (byteCoderCoder) decode(c *codecState, v reflect.Value) {
	if v.Kind() == reflect.Ptr && v.IsNil() {
		return
	}
	m, ok := v.Interface().(ByteCoder)
	if !ok {
		return
	}
	err := m.UnmarshalBytes(&c.Buffer)
	if err != nil {
		c.error(&UnmarshalerError{v.Type(), err})
	}
}

type addrByteCoderCoder struct{}

func (addrByteCoderCoder) encode(c *codecState, v reflect.Value) {
	va := v.Addr()
	if va.IsNil() {
		return
	}
	m := va.Interface().(ByteCoder)
	b, err := m.MarshalBytes()
	if err != nil {
		c.error(&MarshalerError{v.Type(), err})
	}
	c.Write(b)
}

func (addrByteCoderCoder) decode(c *codecState, v reflect.Value) {
	va := v.Addr()
	if va.IsNil() {
		return
	}
	m := va.Interface().(ByteCoder)
	err := m.UnmarshalBytes(&c.Buffer)
	if err != nil {
		c.error(&UnmarshalerError{v.Type(), err})
	}
}

type boolCoder struct{}

func (boolCoder) encode(c *codecState, v reflect.Value) {
	if v.Bool() {
		c.WriteByte(1)
	} else {
		c.WriteByte(0)
	}
}

func (boolCoder) decode(c *codecState, v reflect.Value) {
	if c.ReadByte() == 0 {
		v.SetBool(false)
	} else {
		v.SetBool(true)
	}
}

type int8Coder struct{}

func (int8Coder) encode(c *codecState, v reflect.Value) {
	c.WriteByte(byte(v.Int()))
}

func (int8Coder) decode(c *codecState, v reflect.Value) {
	v.SetInt(int64(int8(c.ReadByte())))
}

type int16Coder struct{}

func (int16Coder) encode(c *codecState, v reflect.Value) {
	i := v.Int()
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, uint16(i))
	c.Write(b)
}

func (int16Coder) decode(c *codecState, v reflect.Value) {
	b := make([]byte, 2)
	c.Read(b)
	i := binary.BigEndian.Uint16(b)
	v.SetInt(int64(int16(i)))
}

type int32Coder struct{}

func (int32Coder) encode(c *codecState, v reflect.Value) {
	i := v.Int()
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, uint32(i))
	c.Write(b)
}

func (int32Coder) decode(c *codecState, v reflect.Value) {
	b := make([]byte, 4)
	c.Read(b)
	i := binary.BigEndian.Uint32(b)
	v.SetInt(int64(int32(i)))
}

type int64Coder struct{}

func (int64Coder) encode(c *codecState, v reflect.Value) {
	i := v.Int()
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(i))
	c.Write(b)
}

func (int64Coder) decode(c *codecState, v reflect.Value) {
	b := make([]byte, 8)
	c.Read(b)
	i := binary.BigEndian.Uint64(b)
	v.SetInt(int64(i))
}

type uint8Coder struct{}

func (uint8Coder) encode(c *codecState, v reflect.Value) {
	c.WriteByte(byte(v.Uint()))
}

func (uint8Coder) decode(c *codecState, v reflect.Value) {
	v.SetUint(uint64(c.ReadByte()))
}

type uint16Coder struct{}

func (uint16Coder) encode(c *codecState, v reflect.Value) {
	u := v.Uint()
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, uint16(u))
	c.Write(b)
}

func (uint16Coder) decode(c *codecState, v reflect.Value) {
	b := make([]byte, 2)
	c.Read(b)
	u := binary.BigEndian.Uint16(b)
	v.SetUint(uint64(u))
}

type uint32Coder struct{}

func (uint32Coder) encode(c *codecState, v reflect.Value) {
	u := v.Uint()
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, uint32(u))
	c.Write(b)
}

func (uint32Coder) decode(c *codecState, v reflect.Value) {
	b := make([]byte, 4)
	c.Read(b)
	u := binary.BigEndian.Uint32(b)
	v.SetUint(uint64(u))
}

type uint64Coder struct{}

func (uint64Coder) encode(c *codecState, v reflect.Value) {
	u := v.Uint()
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, u)
	c.Write(b)
}

func (uint64Coder) decode(c *codecState, v reflect.Value) {
	b := make([]byte, 8)
	c.Read(b)
	u := binary.BigEndian.Uint64(b)
	v.SetUint(u)
}

type float32Coder struct{}

func (float32Coder) encode(c *codecState, v reflect.Value) {
	u := math.Float32bits(float32(v.Float()))
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, u)
	c.Write(b)
}

func (float32Coder) decode(c *codecState, v reflect.Value) {
	b := make([]byte, 4)
	c.Read(b)
	u := binary.BigEndian.Uint32(b)
	f := math.Float32frombits(u)
	v.SetFloat(float64(f))
}

type float64Coder struct{}

func (float64Coder) encode(c *codecState, v reflect.Value) {
	u := math.Float64bits(v.Float())
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, u)
	c.Write(b)
}

func (float64Coder) decode(c *codecState, v reflect.Value) {
	b := make([]byte, 8)
	c.Read(b)
	u := binary.BigEndian.Uint64(b)
	f := math.Float64frombits(u)
	v.SetFloat(f)
}

type DecodeGBKErr struct{ error }

func (e *DecodeGBKErr) Error() string {
	return "bytecodec DecodeGBKErr: " + e.Error()
}

type EncodeGBKErr struct{ error }

func (e *EncodeGBKErr) Error() string {
	return "bytecodec EncodeGBKErr: " + e.Error()
}

type stringCoder struct {
	length   int
	gbk      bool
	gbk18030 bool
	bcd      int // TODO
}

func (sc stringCoder) encodeBytes(v reflect.Value) ([]byte, error) {
	str := v.String()

	var strCodeing encoding.Encoding
	if sc.gbk {
		strCodeing = simplifiedchinese.GB18030
	}
	if sc.gbk18030 {
		strCodeing = simplifiedchinese.GB18030
	}
	if strCodeing != nil {
		textbyte, err := strCodeing.NewEncoder().Bytes([]byte(str))
		if err != nil {
			return textbyte, &EncodeGBKErr{err}
		}
		return textbyte, nil
	}
	return []byte(str), nil
}

func (sc stringCoder) encode(c *codecState, v reflect.Value) {
	b, err := sc.encodeBytes(v)
	if err != nil {
		c.error(err)
	}
	c.Write(b)
}

func (sc stringCoder) decode(c *codecState, v reflect.Value) {
	if sc.length == 0 {
		return
	}
	b := make([]byte, sc.length)
	c.Read(b)

	var strCodeing encoding.Encoding
	if sc.gbk {
		strCodeing = simplifiedchinese.GB18030
	}
	if sc.gbk18030 {
		strCodeing = simplifiedchinese.GB18030
	}
	if strCodeing != nil {
		textbyte, err := strCodeing.NewDecoder().Bytes(b)
		if err != nil {
			c.error(&DecodeGBKErr{err})
		}
		v.SetString(string(textbyte))
		return
	}
	v.SetString(string(b))
}

type interfaceCoder struct{}

func (interfaceCoder) encode(c *codecState, v reflect.Value) {
	if v.IsNil() {
		return
	}
	e := v.Elem()
	valueCodec(e).encode(c, e)
}

func (interfaceCoder) decode(c *codecState, v reflect.Value) {
	if v.IsNil() {
		return
	}
	e := v.Elem()
	valueCodec(e).decode(c, e)
}

type unsupportedTypeCoder struct{}

func (unsupportedTypeCoder) encode(c *codecState, v reflect.Value) {
	c.error(&UnsupportedTypeError{v.Type()})
}

func (unsupportedTypeCoder) decode(c *codecState, v reflect.Value) {
	c.error(&UnsupportedTypeError{v.Type()})
}

type field struct {
	name       string
	index      int
	tagOptions tagOptions
	codec      codec
}

type structFields struct {
	list []field
}

type structCoder struct {
	fields structFields
	length map[int]int
}

func (sc structCoder) encode(c *codecState, v reflect.Value) {
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return
		}
		v = v.Elem()
	}

	for i := range sc.fields.list {
		f := &sc.fields.list[i]
		fv := v.Field(f.index)
		f.codec.encode(c, fv)
	}
}

func (sc structCoder) decode(c *codecState, v reflect.Value) {
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return
		}
		v = v.Elem()
	}

	for i := range sc.fields.list {
		f := &sc.fields.list[i]
		fv := v.Field(f.index)
		f.codec.decode(c, fv)
	}
}

func newStructCoder(t reflect.Type) codec {
	sc := structCoder{fields: cachedTypeFields(t)}
	return sc
}

type arrayCoder struct {
	elemCodec codec
}

func (ae arrayCoder) encode(c *codecState, v reflect.Value) {
	n := v.Len()
	for i := 0; i < n; i++ {
		ae.elemCodec.encode(c, v.Index(i))
	}
}

func (ae arrayCoder) decode(c *codecState, v reflect.Value) {
	n := v.Len()
	for i := 0; i < n; i++ {
		ae.elemCodec.decode(c, v.Index(i))
	}
}

func newArrayCoder(t reflect.Type) codec {
	return arrayCoder{typeCodec(t.Elem())}
}

type ptrCoder struct {
	elemCodec codec
}

func (pe ptrCoder) encode(c *codecState, v reflect.Value) {
	if v.IsNil() {
		return
	}
	if c.ptrLevel++; c.ptrLevel > startDetectingCyclesAfter {
		// We're a large number of nested ptrCoder.encode calls deep;
		// start checking if we've run into a pointer cycle.
		ptr := v.Interface()
		if _, ok := c.ptrSeen[ptr]; ok {
			c.error(&UnsupportedValueError{v, fmt.Sprintf("encountered a cycle via %s", v.Type())})
		}
		c.ptrSeen[ptr] = struct{}{}
		defer delete(c.ptrSeen, ptr)
	}
	pe.elemCodec.encode(c, v.Elem())
	c.ptrLevel--
}

func (pe ptrCoder) decode(c *codecState, v reflect.Value) {
	if c.ptrLevel++; c.ptrLevel > startDetectingCyclesAfter {
		// We're a large number of nested ptrCoder.encode calls deep;
		// start checking if we've run into a pointer cycle.
		ptr := v.Interface()
		if _, ok := c.ptrSeen[ptr]; ok {
			c.error(&UnsupportedValueError{v, fmt.Sprintf("encountered a cycle via %s", v.Type())})
		}
		c.ptrSeen[ptr] = struct{}{}
		defer delete(c.ptrSeen, ptr)
	}
	pe.elemCodec.decode(c, v.Elem())
	c.ptrLevel--
}

func newPtrCoder(t reflect.Type) codec {
	return ptrCoder{typeCodec(t.Elem())}
}

type condAddrCoder struct {
	canAddrC, elseC codec
}

func (ce condAddrCoder) encode(c *codecState, v reflect.Value) {
	if v.CanAddr() {
		ce.canAddrC.encode(c, v)
	} else {
		ce.elseC.encode(c, v)
	}
}

func (ce condAddrCoder) decode(c *codecState, v reflect.Value) {
	if v.CanAddr() {
		ce.canAddrC.decode(c, v)
	} else {
		ce.elseC.decode(c, v)
	}
}

// newCondAddrCoder returns an encoder that checks whether its value
// CanAddr and delegates to canAddrC if so, else to elseC.
func newCondAddrCoder(canAddrC, elseC codec) codec {
	enc := condAddrCoder{canAddrC: canAddrC, elseC: elseC}
	return enc
}

func typeFields(t reflect.Type) structFields {
	var fields []field
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		isUnexported := sf.PkgPath != ""
		if isUnexported || sf.Anonymous {
			continue
		}

		tag := sf.Tag.Get("json")
		if tag == "-" {
			continue
		}

		field := field{
			name:       sf.Name,
			index:      i,
			tagOptions: parseTag(tag),
			codec:      typeCodec(sf.Type),
		}
		fields = append(fields, field)
	}
	return structFields{fields}
}

var fieldCache sync.Map // map[reflect.Type]structFields

// cachedTypeFields is like typeFields but uses a cache to avoid repeated work.
func cachedTypeFields(t reflect.Type) structFields {
	if f, ok := fieldCache.Load(t); ok {
		return f.(structFields)
	}
	f, _ := fieldCache.LoadOrStore(t, typeFields(t))
	return f.(structFields)
}
