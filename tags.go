package bytecodec

import (
	"strconv"
	"strings"
)

type tagOptions struct {
	length   string
	gbk      bool
	gbk18030 bool
	bcd      int // TODO
}

func parseTag(tag string) tagOptions {
	settings := map[string]string{}
	names := strings.Split(tag, ";")
	for _, i := range names {
		s := strings.Split(i, ":")
		if len(s) < 2 {
			continue
		}
		settings[s[0]] = s[1]
	}
	to := tagOptions{}
	to.length = settings["length"]
	if settings["gbk"] != "" {
		to.gbk = true
	}
	if settings["gbk18030"] != "" {
		to.gbk18030 = true
	}
	bcdlength, err := strconv.Atoi(settings["bcd"])
	if err == nil {
		to.bcd = bcdlength
	}
	return to
}
