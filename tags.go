package bytecodec

import (
	"strconv"
	"strings"
)

type tagOptions struct {
	lengthref       string
	length          int // 小于 0 会读取全部剩余字节，默认为 -1
	gbk             bool
	gbk18030        bool
	bcd8421         int
	bcd8421Skipzero bool // 解码时是否跳过数字前面的 0
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

	if bcd, ok := settings["bcd8421"]; ok {
		params := strings.Split(bcd, ",")
		bcdlength, err := strconv.Atoi(params[0])
		if err == nil {
			to.bcd8421 = bcdlength
		}
		if len(params) > 1 {
			if params[1] == "true" {
				to.bcd8421Skipzero = true
			}
		}

	}

	return to
}
