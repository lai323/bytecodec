package bytecodec

import (
	"strconv"
	"strings"
)

type tagOptions struct {
	lengthref string
	length    int // 小于 0 会读取全部剩余字节，默认为 -1
	gbk       bool
	gbk18030  bool
	bcd8421   int
}

func parseTag(tag string) tagOptions {
	settings := map[string]string{}
	names := strings.Split(tag, ";")
	for _, i := range names {
		s := strings.Split(i, ":")
		if len(s) < 2 {
			settings[s[0]] = ""
			continue
		}
		settings[s[0]] = s[1]
	}
	to := tagOptions{}

	to.lengthref = settings["lengthref"]
	to.length = -1
	length := settings["length"]
	l, err := strconv.Atoi(length)
	if err == nil {
		to.length = l
	}

	if _, ok := settings["gbk"]; ok {
		to.gbk = true
	}
	if _, ok := settings["gbk18030"]; ok {
		to.gbk18030 = true
	}

	bcdlength, err := strconv.Atoi(settings["bcd8421"])
	if err == nil {
		to.bcd8421 = bcdlength
	}
	return to
}
