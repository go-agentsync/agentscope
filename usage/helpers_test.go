package usage

import (
	"bufio"
	"io"
)

type bufioReader = bufio.Reader

func newReader(r io.Reader) *bufio.Reader { return bufio.NewReader(r) }
