package bytecodec

import (
	"testing"
)

func TestUnmarshal(t *testing.T) {
	a := All{}
	err := Unmarshal([]byte{}, &a)
	if err != nil {
		t.Error(err)
	}
}
