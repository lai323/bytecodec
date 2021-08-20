
# bytecodec 字节流编解码

这个库实现 `struct` 或其他对象向 `[]byte` 的序列化或反序列化

可以帮助你在编写 tcp 服务，或者需要操作字节流时，简化数据的组包、解包

这个库的组织逻辑 ~~copy~~ 借鉴了标准库 `encoding/json` 🙏

## 安装

使用 `go get` 安装最新版本

`go get -u github.com/lai323/bytecodec`

然后在你的应用中导入

`import "github.com/lai323/bytecodec"`

## 使用

编码时 `bytecodec` 按照 `struct` 的字段顺序将字段一个个写入到 `[]byte` 中；解码时根据字段类型读取对应长度的 `byte` 解析到字段中

嵌入字段和未导出字段会被忽略，也可以使用 `bytecodec:"-"` 标签，主动忽略一个字段

对于 `int` `uint` 被看作 64 位处理

对于空指针字段，编码时不会被忽略，会根据这个指针的类型创建一个空对象，写入到 `[]byte` 中，所以当使用类似下面这种递归类型时，会返回错误，指示不支持这种类型

```go
type s struct {
	Ptr1, Ptr2 *s
}
```

`bytecodec` 支持了可转为 `byte` 所有基础类型，结合下面的几个标签可以轻松的处理一般的字节数据的组包，解包

- `bytecodec:"length:5"` 用于指定不能确定长度的类型的固定长度，对于 `string` 指的是字符串的字节长度，对于 `slice` 指的是元素个数，其他类型会忽略这个标签
- `bytecodec:"lengthref:FieldName"` 用于控制不定长的数据，例如典型的，先从字节流中读取长度，在按这个长度读取后续数据
- `bytecodec:"gbk"` `bytecodec:"gbk18030"` 用于为字符串类型指定编码格式
- `bytecodec:"bcd8421:5,true"` 使用 BCD 压缩，第一个参数是压缩后 byte 长度，不足时在前面填充 0，第二个参数指示解码时，是否跳过首部的 0，这个标签应该使用在字符串类型的字段上，使用字符串表示数值，是为了处理较长的数字串

对于更加复杂的数据结构，你可以实现 `bytecodec.ByteCoder` 自定义编解码

```go
type ByteCoder interface {
	MarshalBytes(*bytecodec.CodecState) error
	UnmarshalBytes(*bytecodec.CodecState) error
}
```

## 例子

```go
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

```
