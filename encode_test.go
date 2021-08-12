package bytecodec

import (
	"fmt"
	"math"
	"reflect"
	"testing"
)

type Small struct {
	Tag string
}

type All struct {
	Bool    bool
	Int     int
	Int8    int8
	Int16   int16
	Int32   int32
	Int64   int64
	Uint    uint
	Uint8   uint8
	Uint16  uint16
	Uint32  uint32
	Uint64  uint64
	Uintptr uintptr
	Float32 float32
	Float64 float64
	String  string

	PBool    *bool
	PInt     *int
	PInt8    *int8
	PInt16   *int16
	PInt32   *int32
	PInt64   *int64
	PUint    *uint
	PUint8   *uint8
	PUint16  *uint16
	PUint32  *uint32
	PUint64  *uint64
	PUintptr *uintptr
	PFloat32 *float32
	PFloat64 *float64
	PString  *string

	Slice   []Small
	SliceP  []*Small
	PSlice  *[]Small
	PSliceP *[]*Small

	EmptySlice []Small
	NilSlice   []Small

	StringSlice []string
	ByteSlice   []byte

	Small   Small
	PSmall  *Small
	PPSmall **Small

	Interface  interface{}
	PInterface *interface{}

	unexported int
}

var allValue = All{
	Bool:        true,
	Int:         -1,
	Int8:        1,
	Int16:       -1,
	Int32:       2,
	Int64:       -1,
	Uint:        7,
	Uint8:       8,
	Uint16:      9,
	Uint32:      10,
	Uint64:      11,
	Uintptr:     12,
	Float32:     14.1,
	Float64:     15.1,
	String:      "16",
	Slice:       []Small{{Tag: "tag20"}, {Tag: "tag21"}},
	SliceP:      []*Small{{Tag: "tag22"}, nil, {Tag: "tag23"}},
	EmptySlice:  []Small{},
	StringSlice: []string{"str24", "str25", "str26"},
	ByteSlice:   []byte{27, 28, 29},
	Small:       Small{Tag: "tag30"},
	PSmall:      &Small{Tag: "tag31"},
	Interface:   5.2,
}

var pallValue = All{
	PBool:      &allValue.Bool,
	PInt:       &allValue.Int,
	PInt8:      &allValue.Int8,
	PInt16:     &allValue.Int16,
	PInt32:     &allValue.Int32,
	PInt64:     &allValue.Int64,
	PUint:      &allValue.Uint,
	PUint8:     &allValue.Uint8,
	PUint16:    &allValue.Uint16,
	PUint32:    &allValue.Uint32,
	PUint64:    &allValue.Uint64,
	PUintptr:   &allValue.Uintptr,
	PFloat32:   &allValue.Float32,
	PFloat64:   &allValue.Float64,
	PString:    &allValue.String,
	PSlice:     &allValue.Slice,
	PSliceP:    &allValue.SliceP,
	PPSmall:    &allValue.PSmall,
	PInterface: &allValue.Interface,
}

var allValueBytes = []byte{
	0x1,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0x1,
	0xff, 0xff,
	0x0, 0x0, 0x0, 0x2,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x7,
	0x8,
	0x0, 0x9,
	0x0, 0x0, 0x0, 0xa,
	0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0xb,
	0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0xc,
	0x41, 0x61, 0x99, 0x9a,
	0x40, 0x2e, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33,
	0x31, 0x36,
	0x74, 0x61, 0x67, 0x32, 0x30,
	0x74, 0x61, 0x67, 0x32, 0x31,
	0x74, 0x61, 0x67, 0x32, 0x32,
	0x74, 0x61, 0x67, 0x32, 0x33,
	0x73, 0x74, 0x72, 0x32, 0x34,
	0x73, 0x74, 0x72, 0x32, 0x35,
	0x73, 0x74, 0x72, 0x32, 0x36,
	0x1b, 0x1c, 0x1d,
	0x74, 0x61, 0x67, 0x33, 0x30,
	0x74, 0x61, 0x67, 0x33, 0x31,
	0x40, 0x14, 0xcc, 0xcc, 0xcc, 0xcc, 0xcc, 0xcd,
}

var pallValueBytes = []byte{
	0x0,
	0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0,
	0x0,
	0x0, 0x0,
	0x0, 0x0, 0x0, 0x0,
	0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0,
	0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0,
	0x0,
	0x0, 0x0,
	0x0, 0x0, 0x0, 0x0,
	0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0,
	0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0,
	0x0, 0x0, 0x0, 0x0,
	0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0,

	0x1,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0x1,
	0xff, 0xff,
	0x0, 0x0, 0x0, 0x2,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x7,
	0x8,
	0x0, 0x9,
	0x0, 0x0, 0x0, 0xa,
	0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0xb,
	0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0xc,
	0x41, 0x61, 0x99, 0x9a,
	0x40, 0x2e, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33,
	0x31, 0x36,
	0x74, 0x61, 0x67, 0x32, 0x30,
	0x74, 0x61, 0x67, 0x32, 0x31,
	0x74, 0x61, 0x67, 0x32, 0x32,
	0x74, 0x61, 0x67, 0x32, 0x33,
	0x74, 0x61, 0x67, 0x33, 0x31,
	0x40, 0x14, 0xcc, 0xcc, 0xcc, 0xcc, 0xcc, 0xcd,
}

func TestMarshal(t *testing.T) {
	b, err := Marshal(allValue)
	if err != nil {
		t.Fatalf("Marshal allValue error: %v", err)
	}
	get := fmt.Sprintf("%#v", b)
	want := fmt.Sprintf("%#v", allValueBytes)
	if get != want {
		t.Errorf("Marshal allValue = %s should be %s", get, want)
		return
	}

	b, err = Marshal(pallValue)
	if err != nil {
		t.Fatalf("Marshal pallValue error: %v", err)
	}
	get = fmt.Sprintf("%#v", b)
	want = fmt.Sprintf("%#v", pallValueBytes)
	if get != want {
		t.Errorf("Marshal pallValue = %s should be %s", get, want)
		return
	}
}

type simpleByteCoder struct {
	s string
}

func (bc simpleByteCoder) MarshalBytes(cs *CodecState) error {
	for _, b := range []byte(bc.s) {
		cs.WriteByte(b + 1)
	}
	return nil
}
func (bc *simpleByteCoder) UnmarshalBytes(cs *CodecState) error {
	var sb []byte
	for _, b := range cs.Bytes() {
		sb = append(sb, b-1)
	}
	bc.s = string(sb)
	return nil
}

var byteCoderTests = []struct {
	b   []byte
	v   interface{}
	ptr interface{}
}{{
	[]byte{231, 182, 140, 233, 176, 150},
	&simpleByteCoder{"测试"},
	&simpleByteCoder{""},
}, {
	[]byte{117, 102, 116, 117},
	&simpleByteCoder{"test"},
	&simpleByteCoder{""},
}}

func TestByteCoder(t *testing.T) {
	for _, tt := range byteCoderTests {
		b, err := Marshal(tt.v)
		if err != nil {
			t.Errorf("Marshal %#v, unexpected error: %#v", tt.v, err)
			continue
		}
		if !reflect.DeepEqual(tt.b, b) {
			t.Errorf("Marshal %#v = %#v, want %#v", tt.v, b, tt.b)
		}

		err = Unmarshal(tt.b, tt.ptr)
		if err != nil {
			t.Errorf("Unmarshal %#v, unexpected error: %#v", tt.b, err)
			continue
		}
		if !reflect.DeepEqual(tt.ptr, tt.v) {
			t.Errorf("Unmarshal %#v = %#v, want %#v", tt.b, tt.ptr, tt.v)
		}
	}
}

type SamePointerNoCycle struct {
	Ptr1, Ptr2 *SamePointerNoCycle
}

var samePointerNoCycle = &SamePointerNoCycle{}

type PointerCycle struct {
	Ptr *PointerCycle
}

var pointerCycle = &PointerCycle{}

type PointerCycleIndirect struct {
	Ptrs []interface{}
}

var pointerCycleIndirect = &PointerCycleIndirect{}

func init() {
	ptr := &SamePointerNoCycle{}
	samePointerNoCycle.Ptr1 = ptr
	samePointerNoCycle.Ptr2 = ptr

	pointerCycle.Ptr = pointerCycle
	pointerCycleIndirect.Ptrs = []interface{}{pointerCycleIndirect}
}

// 测试了递归结构
func TestSamePointerNoCycle(t *testing.T) {
	if _, err := Marshal(samePointerNoCycle); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

var unsupportedValues = []interface{}{
	math.NaN(),
	math.Inf(-1),
	math.Inf(1),
	pointerCycle,
	pointerCycleIndirect,
}

func TestUnsupportedValues(t *testing.T) {
	for _, v := range unsupportedValues {
		if _, err := Marshal(v); err != nil {
			if _, ok := err.(*UnsupportedValueError); !ok {
				t.Errorf("for %v, got %T want UnsupportedValueError", v, err)
			}
		} else {
			t.Errorf("for %v, expected error", v)
		}
	}
}
