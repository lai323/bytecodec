package main

import (
	"fmt"
	"time"

	"github.com/lai323/bcd8421"
	"github.com/lai323/bytecodec"
)

// 实现 bytecodec.ByteCoder 自定义时间字段的编解码
// 使用 BCD 压缩时间
var timeformat = "060102150405" // 2006-01-02 15:04:05
type BCDTime time.Time

func (bt BCDTime) MarshalBytes(cs *bytecodec.CodecState) error {
	tstr := bt.String()
	b, err := bcd8421.EncodeFromStr(tstr, 6)
	if err != nil {
		return err
	}
	cs.Write(b)
	return nil
}

func (bt *BCDTime) UnmarshalBytes(cs *bytecodec.CodecState) error {
	b := make([]byte, 6)
	cs.ReadFull(b)
	tstr, err := bcd8421.DecodeToStr(b, false)
	if err != nil {
		return err
	}

	t, err := time.ParseInLocation(timeformat, tstr, time.Local)
	if err != nil {
		return err
	}

	*bt = BCDTime(t)
	return nil
}

func (bt BCDTime) String() string {
	return time.Time(bt).Format(timeformat)
}

type Header struct {
	SerialNo uint16
	Time     BCDTime
}

type Packet struct {
	Header    Header
	Phone     string `bytecodec:"bcd8421:6,true"` // 使用长度为 6 的 BCD 8421 编码，解码时跳过数字前面的 0
	MsgLength uint8  `bytecodec:"lengthref:Msg"`  // 表示这个字段的值是 Msg 的字节长度
	Msg       string `bytecodec:"gbk"`            // 使用 GBK 编码
}

func (p Packet) String() string {
	return fmt.Sprintf("<SerialNo:%d,Time:%s,Phone:%s,MsgLength:%d,Msg:%s>", p.Header.SerialNo, p.Header.Time, p.Phone, p.MsgLength, p.Msg)
}

func marshal() {
	t := BCDTime(time.Date(2006, 01, 02, 15, 04, 05, 0, time.Local))
	p := Packet{
		Header: Header{
			SerialNo: 1,
			Time:     t,
		},
		Phone: "18102169375",
		Msg:   "你好",
	}
	b, err := bytecodec.Marshal(p)
	fmt.Println(fmt.Sprintf("%#v", b))
	fmt.Println(err)
}

func unmarshal() {
	b := []byte{
		0x0, 0x1,
		0x6, 0x1, 0x2, 0x15, 0x4, 0x5,
		0x1, 0x81, 0x2, 0x16, 0x93, 0x75,
		0x4,
		0xc4, 0xe3, 0xba, 0xc3,
	}
	out := &Packet{}
	err := bytecodec.Unmarshal(b, out)
	fmt.Println(fmt.Sprintf("%v", out))
	fmt.Println(err)
}

func main() {
	marshal()
	// []byte{
	//     0x0, 0x1,
	//     0x6, 0x1, 0x2, 0x15, 0x4, 0x5,
	//     0x1, 0x81, 0x2, 0x16, 0x93, 0x75,
	//     0x4,
	//     0xc4, 0xe3, 0xba, 0xc3,
	// }
	// <nil>

	unmarshal()
	// <SerialNo:1,Time:060102150405,Phone:18102169375,MsgLength:4,Msg:你好>
	// <nil>
}
