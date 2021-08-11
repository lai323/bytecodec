package bytecodec

import (
	"fmt"
	"testing"
)

type tmp struct {
	l int `bytecodec:"lengthref:s"`
	s string
}

func TestPtrCoder(t *testing.T) {
	b := true
	var bp *bool = &b
	type BP struct {
		BP *bool
	}
	bpobj := BP{&b}
	bytes, err := Marshal(bp)
	fmt.Println(bytes, err)
	bytes, err = Marshal(bpobj)
	fmt.Println(bytes, err)
}
