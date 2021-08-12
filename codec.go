package bytecodec

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"sync"

	"github.com/lai323/bcd8421"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/simplifiedchinese"
)

type ByteCoder interface {
	MarshalBytes(*CodecState) error
	UnmarshalBytes(*CodecState) error
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

type pointerTrack struct {
	// Keep track of what pointers we've seen in the current recursive call
	// path, to avoid cycles that could lead to a stack overflow. Only do
	// the relatively expensive map operations if ptrLevel is larger than
	// startDetectingCyclesAfter, so that we skip the work if we're within a
	// reasonable amount of nested pointers deep.
	ptrLevel uint
	ptrSeen  map[interface{}]struct{}
}

func newPointerTrack() pointerTrack {
	return pointerTrack{ptrSeen: make(map[interface{}]struct{})}
}

type CodecState struct {
	bytes.Buffer
	pt *pointerTrack
}

const startDetectingCyclesAfter = 1000

var encodeStatePool sync.Pool

func newCodecState() *CodecState {
	pt := newPointerTrack()
	return subCodecState(&pt)
}

func subCodecState(pt *pointerTrack) *CodecState {
	if v := encodeStatePool.Get(); v != nil {
		e := v.(*CodecState)
		e.Reset()
		e.pt = pt
		return e
	}
	return &CodecState{pt: pt}
}

func (c *CodecState) marshal(v interface{}) error {
	vv := reflect.ValueOf(v)
	return c.code(valueCodec(vv).encode, vv)
}

func (c *CodecState) unmarshal(v reflect.Value) error {
	return c.code(valueCodec(v).decode, v)
}

func (c *CodecState) gensub() *CodecState {
	return subCodecState(c.pt)
}

type bytecodecError struct{ error }

func (c *CodecState) code(f func(e *CodecState, v reflect.Value, to tagOptions), v reflect.Value) (err error) {
	defer func() {
		if r := recover(); r != nil {
			if be, ok := r.(bytecodecError); ok {
				err = be.error
			} else {
				panic(r)
			}
		}
	}()
	f(c, v, tagOptions{})
	return nil
}

func (c *CodecState) error(err error) {
	panic(bytecodecError{err})
}

var DataLengthErr = errors.New("Not enough data length")

func (c *CodecState) Read(p []byte) int {
	n, err := c.Buffer.Read(p)
	if err != nil {
		c.error(bytecodecError{DataLengthErr})
	}
	return n
}

func (c *CodecState) ReadByte() byte {
	b, err := c.Buffer.ReadByte()
	if err != nil {
		c.error(bytecodecError{DataLengthErr})
	}
	return b
}

type codec interface {
	encode(e *CodecState, v reflect.Value, to tagOptions)
	decode(e *CodecState, v reflect.Value, to tagOptions)
	typ() reflect.Kind
}

var codecCache sync.Map // map[reflect.Type]codec

func valueCodec(v reflect.Value) codec {
	if !v.IsValid() {
		return invalidValueCoder{}
	}
	return typeCodec(v.Type())
}

// func typeCodec(t reflect.Type) codec {
// 	if fi, ok := codecCache.Load(t); ok {
// 		return fi.(codec)
// 	}
// 	c := newTypeCodec(t, true)
// 	codecCache.Store(t, c)
// 	return c
// }

type recursiveWrapCoder struct {
	elemCodec *codec
	wg        *sync.WaitGroup
}

func (rw recursiveWrapCoder) typ() reflect.Kind {
	return (*rw.elemCodec).typ()
}

func (rw recursiveWrapCoder) encode(c *CodecState, v reflect.Value, to tagOptions) {
	rw.wg.Wait()
	(*rw.elemCodec).encode(c, v, to)
}

func (rw recursiveWrapCoder) decode(c *CodecState, v reflect.Value, to tagOptions) {
	rw.wg.Wait()
	(*rw.elemCodec).decode(c, v, to)
}

func typeCodec(t reflect.Type) codec {
	if ci, ok := codecCache.Load(t); ok {
		return ci.(codec)
	}

	// To deal with recursive types, populate the map with an
	// indirect codec before we build it. This type waits on the
	// real codec to be ready and then calls it. This indirect
	// codec is only used for recursive types.
	var (
		wg  sync.WaitGroup
		tmp codec
	)
	cp := &tmp
	wg.Add(1)
	ci, loaded := codecCache.LoadOrStore(t, recursiveWrapCoder{cp, &wg})
	if loaded {
		return ci.(codec)
	}

	// Compute the real coder and replace the indirect func with it.
	tmp = newTypeCodec(t, true)
	wg.Done()
	codecCache.Store(t, tmp)
	return tmp
}

var (
	bytecoderType = reflect.TypeOf((*ByteCoder)(nil)).Elem()
)

func newTypeCodec(t reflect.Type, allowAddr bool) codec {
	if t.Kind() != reflect.Ptr && allowAddr && reflect.PtrTo(t).Implements(bytecoderType) {
		return newCondAddrCoder(addrByteCoderCoder{}, newTypeCodec(t, false))
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
	case reflect.Array:
		return newArrayCoder(t)
	case reflect.Slice:
		return newSliceCoder(t)
	case reflect.Ptr:
		return newPtrCoder(t)
	default:
		return unsupportedTypeCoder{}
	}
}

type invalidValueCoder struct {
}

func (invalidValueCoder) typ() reflect.Kind {
	return reflect.Invalid
}

func (invalidValueCoder) encode(c *CodecState, v reflect.Value, _ tagOptions) {
}

func (invalidValueCoder) decode(c *CodecState, v reflect.Value, _ tagOptions) {
}

type byteCoderCoder struct{}

func (byteCoderCoder) typ() reflect.Kind {
	return reflect.Invalid
}

func (byteCoderCoder) encode(c *CodecState, v reflect.Value, _ tagOptions) {
	m, ok := v.Interface().(ByteCoder)
	if !ok {
		return
	}
	err := m.MarshalBytes(c)
	if err != nil {
		c.error(&MarshalerError{v.Type(), err})
	}
}

func (byteCoderCoder) decode(c *CodecState, v reflect.Value, _ tagOptions) {
	m, ok := v.Interface().(ByteCoder)
	if !ok {
		return
	}
	err := m.UnmarshalBytes(c)
	if err != nil {
		c.error(&UnmarshalerError{v.Type(), err})
	}
}

type addrByteCoderCoder struct{}

func (addrByteCoderCoder) typ() reflect.Kind {
	return reflect.Invalid
}

func (addrByteCoderCoder) encode(c *CodecState, v reflect.Value, _ tagOptions) {
	va := v.Addr()
	if va.IsNil() {
		return
	}
	m := va.Interface().(ByteCoder)
	err := m.MarshalBytes(c)
	if err != nil {
		c.error(&MarshalerError{v.Type(), err})
	}
}

func (addrByteCoderCoder) decode(c *CodecState, v reflect.Value, _ tagOptions) {
	va := v.Addr()
	if va.IsNil() {
		return
	}
	m := va.Interface().(ByteCoder)
	err := m.UnmarshalBytes(c)
	if err != nil {
		c.error(&UnmarshalerError{v.Type(), err})
	}
}

type boolCoder struct{}

func (boolCoder) typ() reflect.Kind {
	return reflect.Bool
}

func (boolCoder) encode(c *CodecState, v reflect.Value, _ tagOptions) {
	if v.Bool() {
		c.WriteByte(1)
	} else {
		c.WriteByte(0)
	}
}

func (boolCoder) decode(c *CodecState, v reflect.Value, _ tagOptions) {
	if c.ReadByte() == 0 {
		v.SetBool(false)
	} else {
		v.SetBool(true)
	}
}

type int8Coder struct{}

func (int8Coder) typ() reflect.Kind {
	return reflect.Int8
}

func (int8Coder) encode(c *CodecState, v reflect.Value, _ tagOptions) {
	c.WriteByte(byte(v.Int()))
}

func (int8Coder) decode(c *CodecState, v reflect.Value, _ tagOptions) {
	v.SetInt(int64(int8(c.ReadByte())))
}

type int16Coder struct{}

func (int16Coder) typ() reflect.Kind {
	return reflect.Int16
}

func (int16Coder) encode(c *CodecState, v reflect.Value, _ tagOptions) {
	i := v.Int()
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, uint16(i))
	c.Write(b)
}

func (int16Coder) decode(c *CodecState, v reflect.Value, _ tagOptions) {
	b := make([]byte, 2)
	c.Read(b)
	i := binary.BigEndian.Uint16(b)
	v.SetInt(int64(int16(i)))
}

type int32Coder struct{}

func (int32Coder) typ() reflect.Kind {
	return reflect.Int32
}

func (int32Coder) encode(c *CodecState, v reflect.Value, _ tagOptions) {
	i := v.Int()
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, uint32(i))
	c.Write(b)
}

func (int32Coder) decode(c *CodecState, v reflect.Value, _ tagOptions) {
	b := make([]byte, 4)
	c.Read(b)
	i := binary.BigEndian.Uint32(b)
	v.SetInt(int64(int32(i)))
}

type int64Coder struct{}

func (int64Coder) typ() reflect.Kind {
	return reflect.Int64
}

func (int64Coder) encode(c *CodecState, v reflect.Value, _ tagOptions) {
	i := v.Int()
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(i))
	c.Write(b)
}

func (int64Coder) decode(c *CodecState, v reflect.Value, _ tagOptions) {
	b := make([]byte, 8)
	c.Read(b)
	i := binary.BigEndian.Uint64(b)
	v.SetInt(int64(i))
}

type uint8Coder struct{}

func (uint8Coder) typ() reflect.Kind {
	return reflect.Uint8
}

func (uint8Coder) encode(c *CodecState, v reflect.Value, _ tagOptions) {
	c.WriteByte(byte(v.Uint()))
}

func (uint8Coder) decode(c *CodecState, v reflect.Value, _ tagOptions) {
	v.SetUint(uint64(c.ReadByte()))
}

type uint16Coder struct{}

func (uint16Coder) typ() reflect.Kind {
	return reflect.Uint16
}

func (uint16Coder) encode(c *CodecState, v reflect.Value, _ tagOptions) {
	u := v.Uint()
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, uint16(u))
	c.Write(b)
}

func (uint16Coder) decode(c *CodecState, v reflect.Value, _ tagOptions) {
	b := make([]byte, 2)
	c.Read(b)
	u := binary.BigEndian.Uint16(b)
	v.SetUint(uint64(u))
}

type uint32Coder struct{}

func (uint32Coder) typ() reflect.Kind {
	return reflect.Uint32
}

func (uint32Coder) encode(c *CodecState, v reflect.Value, _ tagOptions) {
	u := v.Uint()
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, uint32(u))
	c.Write(b)
}

func (uint32Coder) decode(c *CodecState, v reflect.Value, _ tagOptions) {
	b := make([]byte, 4)
	c.Read(b)
	u := binary.BigEndian.Uint32(b)
	v.SetUint(uint64(u))
}

type uint64Coder struct{}

func (uint64Coder) typ() reflect.Kind {
	return reflect.Uint64
}

func (uint64Coder) encode(c *CodecState, v reflect.Value, _ tagOptions) {
	u := v.Uint()
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, u)
	c.Write(b)
}

func (uint64Coder) decode(c *CodecState, v reflect.Value, _ tagOptions) {
	b := make([]byte, 8)
	c.Read(b)
	u := binary.BigEndian.Uint64(b)
	v.SetUint(u)
}

type float32Coder struct{}

func (float32Coder) typ() reflect.Kind {
	return reflect.Float32
}

func (float32Coder) encode(c *CodecState, v reflect.Value, _ tagOptions) {
	f := v.Float()
	if math.IsInf(f, 0) || math.IsNaN(f) {
		c.error(&UnsupportedValueError{v, strconv.FormatFloat(f, 'g', -1, 32)})
	}

	u := math.Float32bits(float32(f))
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, u)
	c.Write(b)
}

func (float32Coder) decode(c *CodecState, v reflect.Value, _ tagOptions) {
	b := make([]byte, 4)
	c.Read(b)
	u := binary.BigEndian.Uint32(b)
	f := math.Float32frombits(u)
	v.SetFloat(float64(f))
}

type float64Coder struct{}

func (float64Coder) typ() reflect.Kind {
	return reflect.Float64
}

func (float64Coder) encode(c *CodecState, v reflect.Value, _ tagOptions) {
	f := v.Float()
	if math.IsInf(f, 0) || math.IsNaN(f) {
		c.error(&UnsupportedValueError{v, strconv.FormatFloat(f, 'g', -1, 64)})
	}

	u := math.Float64bits(f)
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, u)
	c.Write(b)
}

func (float64Coder) decode(c *CodecState, v reflect.Value, _ tagOptions) {
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

type EncodeBCDErr struct{ error }

func (e *EncodeBCDErr) Error() string {
	return "bytecodec EncodeBCDErr: " + e.Error()
}

type DecodeBCDErr struct{ error }

func (e *DecodeBCDErr) Error() string {
	return "bytecodec DecodeBCDErr: " + e.Error()
}

type TagErr struct{ error }

func (e *TagErr) Error() string {
	return "bytecodec TagErr: " + e.Error()
}

type LengthErr struct{ error }

func (e *LengthErr) Error() string {
	return "bytecodec LengthErr: " + e.Error()
}

type stringCoder struct {
}

func (stringCoder) typ() reflect.Kind {
	return reflect.String
}

func (sc stringCoder) encode(c *CodecState, v reflect.Value, to tagOptions) {
	str := v.String()
	var length int

	if to.bcd != 0 {
		b, err := bcd8421.EncodeFromStr(str, to.bcd)
		if err != nil {
			c.error(&EncodeBCDErr{err})
		}
		length, _ = c.Write(b)
		return
	}

	var strCodeing encoding.Encoding
	if to.gbk {
		strCodeing = simplifiedchinese.GBK
	}
	if to.gbk18030 {
		strCodeing = simplifiedchinese.GB18030
	}
	if strCodeing != nil {
		b, err := strCodeing.NewEncoder().Bytes([]byte(str))
		if err != nil {
			c.error(&EncodeGBKErr{err})
		}
		length, _ = c.Write(b)
		return
	}

	length, _ = c.Write([]byte(str))
	if to.length != 0 && length != to.length {
		c.error(&LengthErr{fmt.Errorf("string length %d tag length %d", length, to.length)})
	}
}

func (sc stringCoder) decode(c *CodecState, v reflect.Value, to tagOptions) {
	var b []byte
	if to.length != 0 {
		b = make([]byte, to.length)
		c.Read(b)
	} else {
		b = c.Bytes()
	}

	if to.bcd != 0 {
		sb, err := bcd8421.DecodeToStr(b)
		if err != nil {
			c.error(&DecodeBCDErr{err})
		}
		v.SetString(string(sb))
		return
	}

	var strCodeing encoding.Encoding
	if to.gbk {
		strCodeing = simplifiedchinese.GBK
	}
	if to.gbk18030 {
		strCodeing = simplifiedchinese.GB18030
	}
	if strCodeing != nil {
		sb, err := strCodeing.NewDecoder().Bytes(b)
		if err != nil {
			c.error(&DecodeGBKErr{err})
		}
		v.SetString(string(sb))
		return
	}
	v.SetString(string(b))
}

type interfaceCoder struct{}

func (interfaceCoder) typ() reflect.Kind {
	return reflect.Interface
}

func (interfaceCoder) encode(c *CodecState, v reflect.Value, to tagOptions) {
	if v.IsNil() {
		return
	}
	e := v.Elem()
	valueCodec(e).encode(c, e, to)
}

func (interfaceCoder) decode(c *CodecState, v reflect.Value, to tagOptions) {
	if v.IsNil() {
		return
	}
	e := v.Elem()
	valueCodec(e).decode(c, e, to)
}

type unsupportedTypeCoder struct{}

func (unsupportedTypeCoder) typ() reflect.Kind {
	return reflect.Invalid
}

func (unsupportedTypeCoder) encode(c *CodecState, v reflect.Value, _ tagOptions) {
	c.error(&UnsupportedTypeError{v.Type()})
}

func (unsupportedTypeCoder) decode(c *CodecState, v reflect.Value, _ tagOptions) {
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
}

func (structCoder) typ() reflect.Kind {
	return reflect.Struct
}

func (sc structCoder) encode(c *CodecState, v reflect.Value, _ tagOptions) {
	buf := make([][]byte, len(sc.fields.list))

	for i := range sc.fields.list {
		f := sc.fields.list[i]
		fv := v.Field(f.index)

		if f.tagOptions.lengthref != "" {
			found, ref, refindex := sc.findref(f)
			if !found {
				c.error(&TagErr{fmt.Errorf("lengthref %s not fount field %s", f.name, f.tagOptions.lengthref)})
			}

			refv := v.Field(ref.index)
			err := sc.encodeLengthref(c, f, ref, i, refindex, refv, buf)
			if err != nil {
				c.error(err)
			}
			continue
		}
		if sc.existLengthref(f) {
			continue
		}

		scc := c.gensub()
		f.codec.encode(scc, fv, f.tagOptions)
		buf[i] = append([]byte(nil), scc.Bytes()...)
		encodeStatePool.Put(scc)
	}
	c.Write(bytes.Join(buf, []byte{}))
}

func (sc structCoder) findref(f field) (found bool, ref field, refindex int) {
	for index, item := range sc.fields.list {
		if f.tagOptions.lengthref == item.tagOptions.lengthref {
			ref = item
			found = true
			refindex = index
		}
	}
	return
}

func (sc structCoder) existLengthref(f field) bool {
	for _, i := range sc.fields.list {
		if f.name == i.tagOptions.lengthref {
			return true
		}
	}
	return false
}

func (sc structCoder) encodeLengthref(c *CodecState, lengthref, ref field, lengthrefIndex, refIndex int, refv reflect.Value, buf [][]byte) error {
	scc := c.gensub()
	ref.codec.encode(scc, refv, ref.tagOptions)
	refbytes := append([]byte(nil), scc.Bytes()...)
	encodeStatePool.Put(scc)

	length := len(refbytes)
	var lengthv reflect.Value

	switch lengthref.codec.typ() {
	case reflect.Int8:
		lengthv = reflect.ValueOf(int8(length))
	case reflect.Int16:
		lengthv = reflect.ValueOf(int16(length))
	case reflect.Int32:
		lengthv = reflect.ValueOf(int32(length))
	case reflect.Int, reflect.Int64:
		lengthv = reflect.ValueOf(int64(length))
	case reflect.Uint8:
		lengthv = reflect.ValueOf(uint8(length))
	case reflect.Uint16:
		lengthv = reflect.ValueOf(uint16(length))
	case reflect.Uint32:
		lengthv = reflect.ValueOf(uint32(length))
	case reflect.Uint, reflect.Uint64, reflect.Uintptr:
		lengthv = reflect.ValueOf(uint64(length))
	case reflect.Float32:
		lengthv = reflect.ValueOf(float32(length))
	case reflect.Float64:
		lengthv = reflect.ValueOf(float64(length))
	default:
		return &TagErr{fmt.Errorf("lengthref %s type %s is invalid", lengthref.name, lengthref.codec.typ())}
	}

	scc = c.gensub()
	lengthref.codec.encode(scc, lengthv, lengthref.tagOptions)
	lengthrefbytes := append([]byte(nil), scc.Bytes()...)
	encodeStatePool.Put(scc)

	buf[lengthrefIndex] = lengthrefbytes
	buf[refIndex] = refbytes
	return nil
}

func (sc structCoder) decode(c *CodecState, v reflect.Value, _ tagOptions) {

	for i := range sc.fields.list {
		f := sc.fields.list[i]
		if f.tagOptions.lengthref != "" {

			found, _, refindex := sc.findref(f)
			if !found {
				c.error(&TagErr{fmt.Errorf("lengthref %s not fount field %s", f.name, f.tagOptions.lengthref)})
			}
			var length int
			fv := v.Field(f.index)
			switch f.codec.typ() {
			case reflect.Int8:
				fallthrough
			case reflect.Int16:
				fallthrough
			case reflect.Int32:
				fallthrough
			case reflect.Int, reflect.Int64:
				length = int(fv.Int())
			case reflect.Uint8:
				fallthrough
			case reflect.Uint16:
				fallthrough
			case reflect.Uint32:
				fallthrough
			case reflect.Uint, reflect.Uint64, reflect.Uintptr:
				length = int(fv.Uint())
			case reflect.Float32:
				fallthrough
			case reflect.Float64:
				length = int(fv.Float())
			default:
				c.error(&TagErr{fmt.Errorf("lengthref %s type %s is invalid", f.name, f.codec.typ())})
			}

			sc.fields.list[refindex].tagOptions.length = length
		}
	}

	for i := range sc.fields.list {
		f := &sc.fields.list[i]
		fv := v.Field(f.index)
		f.codec.decode(c, fv, f.tagOptions)
	}
}

func newStructCoder(t reflect.Type) codec {
	sc := structCoder{fields: cachedTypeFields(t)}
	return sc
}

type arrayCoder struct {
	elemCodec codec
}

func (arrayCoder) typ() reflect.Kind {
	return reflect.Array
}

func (ac arrayCoder) encode(c *CodecState, v reflect.Value, to tagOptions) {
	n := v.Len()
	pl := c.Len()

	for i := 0; i < n; i++ {
		ac.elemCodec.encode(c, v.Index(i), to)
	}

	length := c.Len() - pl
	if to.length != 0 && length != to.length {
		c.error(&LengthErr{fmt.Errorf("array length %d tag length %d", length, to.length)})
	}
}

func (ac arrayCoder) decode(c *CodecState, v reflect.Value, to tagOptions) {
	var b []byte
	if to.length != 0 {
		b = make([]byte, to.length)
		c.Read(b)
	} else {
		b = c.Bytes()
	}
	scc := c.gensub()
	scc.Write(b)

	i := 0
	for {
		if i < v.Len() {
			ac.elemCodec.decode(scc, v.Index(i), to)
			if scc.Len() == 0 {
				break
			}
			continue
		}
		break
	}

	if i < v.Len() {
		z := reflect.Zero(v.Type().Elem())
		for ; i < v.Len(); i++ {
			v.Index(i).Set(z)
		}
	}
	encodeStatePool.Put(scc)
}

func newArrayCoder(t reflect.Type) codec {
	return arrayCoder{typeCodec(t.Elem())}
}

type sliceCoder struct {
	elemCodec codec
}

func (sliceCoder) typ() reflect.Kind {
	return reflect.Slice
}

func (sc sliceCoder) encode(c *CodecState, v reflect.Value, to tagOptions) {
	n := v.Len()
	pl := c.Len()

	for i := 0; i < n; i++ {
		sc.elemCodec.encode(c, v.Index(i), to)
	}

	length := c.Len() - pl
	if to.length != 0 && length != to.length {
		c.error(&LengthErr{fmt.Errorf("slice length %d tag length %d", length, to.length)})
	}
}

func (sc sliceCoder) decode(c *CodecState, v reflect.Value, to tagOptions) {
	var b []byte
	if to.length != 0 {
		b = make([]byte, to.length)
		c.Read(b)
	} else {
		b = c.Bytes()
	}
	scc := c.gensub()
	scc.Write(b)

	i := 0
	for {
		// Grow slice if necessary
		if i >= v.Cap() {
			newcap := v.Cap() + v.Cap()/2
			if newcap < 4 {
				newcap = 4
			}
			newv := reflect.MakeSlice(v.Type(), v.Len(), newcap)
			reflect.Copy(newv, v)
			v.Set(newv)
		}
		if i >= v.Len() {
			v.SetLen(i + 1)
		}

		sc.elemCodec.decode(scc, v.Index(i), to)
		if scc.Len() != 0 {
			continue
		}
		break
	}

	if i == 0 {
		v.Set(reflect.MakeSlice(v.Type(), 0, 0))
	}
	encodeStatePool.Put(scc)
}

func newSliceCoder(t reflect.Type) codec {
	return sliceCoder{typeCodec(t.Elem())}
}

type ptrCoder struct {
	elemCodec codec
}

func (ptrCoder) typ() reflect.Kind {
	return reflect.Ptr
}

func (pe ptrCoder) encode(c *CodecState, v reflect.Value, to tagOptions) {
	if v.IsNil() {
		return
	}
	if c.pt.ptrLevel++; c.pt.ptrLevel > startDetectingCyclesAfter {
		// We're a large number of nested ptrCoder.encode calls deep;
		// start checking if we've run into a pointer cycle.
		ptr := v.Interface()
		if _, ok := c.pt.ptrSeen[ptr]; ok {
			c.error(&UnsupportedValueError{v, fmt.Sprintf("encountered a cycle via %s", v.Type())})
		}
		c.pt.ptrSeen[ptr] = struct{}{}
		defer delete(c.pt.ptrSeen, ptr)
	}
	pe.elemCodec.encode(c, v.Elem(), to)
	c.pt.ptrLevel--
}

func (pe ptrCoder) decode(c *CodecState, v reflect.Value, to tagOptions) {
	if v.IsNil() {
		v.Set(reflect.New(v.Type().Elem()))
	}

	if c.pt.ptrLevel++; c.pt.ptrLevel > startDetectingCyclesAfter {
		// We're a large number of nested ptrCoder.encode calls deep;
		// start checking if we've run into a pointer cycle.
		ptr := v.Interface()
		if _, ok := c.pt.ptrSeen[ptr]; ok {
			c.error(&UnsupportedValueError{v, fmt.Sprintf("encountered a cycle via %s", v.Type())})
		}
		c.pt.ptrSeen[ptr] = struct{}{}
		defer delete(c.pt.ptrSeen, ptr)
	}
	pe.elemCodec.decode(c, v.Elem(), to)
	c.pt.ptrLevel--
}

func newPtrCoder(t reflect.Type) codec {
	return ptrCoder{typeCodec(t.Elem())}
}

type condAddrCoder struct {
	canAddrC, elseC codec
}

func (condAddrCoder) typ() reflect.Kind {
	return reflect.Invalid
}

func (ce condAddrCoder) encode(c *CodecState, v reflect.Value, to tagOptions) {
	if v.CanAddr() {
		ce.canAddrC.encode(c, v, to)
	} else {
		ce.elseC.encode(c, v, to)
	}
}

func (ce condAddrCoder) decode(c *CodecState, v reflect.Value, to tagOptions) {
	if v.CanAddr() {
		ce.canAddrC.decode(c, v, to)
	} else {
		ce.elseC.decode(c, v, to)
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
