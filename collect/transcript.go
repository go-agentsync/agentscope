package collect

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strconv"
	"time"
	"unicode/utf8"
)

// A transcript is untrusted input: it is written by a process that also holds
// whatever a prompt-injected page told it, it is hundreds of megabytes, and
// one line of it can be a whole tool result. So it is read as follows.
//
//   - Only the tail (Options.TailBytes) is read the first time a file is seen;
//     after that only what was appended, from the remembered offset.
//   - A line longer than Options.MaxLine is skipped, counted, and reported.
//   - A line is decoded into a struct that has NO field for message text, so
//     the prompt and the response are never copied into memory.
//   - A line that is not complete (no newline yet) is left for the next pass.

// tstate is what is remembered about one transcript between passes.
type tstate struct {
	offset  int64
	size    int64
	cwd     string
	branch  string
	slug    string
	title   string
	custom  string
	pr      string
	lastTS  time.Time
	sent    int
	sentTo  map[string]int
	skipped int
	bad     int
}

// maxSentTo bounds the per-session recipient map.
const maxSentTo = 256

// rawLine is the part of a transcript event that is metadata. There is
// deliberately no field for message content.
type rawLine struct {
	Type        string `json:"type"`
	Timestamp   string `json:"timestamp"`
	Cwd         string `json:"cwd"`
	GitBranch   string `json:"gitBranch"`
	Slug        string `json:"slug"`
	AITitle     string `json:"aiTitle"`
	CustomTitle string `json:"customTitle"`
	PRNumber    int    `json:"prNumber"`
	PRRepo      string `json:"prRepository"`
}

// rawTools is read only for assistant lines that mention SendMessage, and only
// the tool name and its recipient are decoded.
type rawTools struct {
	Message struct {
		Content []struct {
			Type  string `json:"type"`
			Name  string `json:"name"`
			Input struct {
				To string `json:"to"`
			} `json:"input"`
		} `json:"content"`
	} `json:"message"`
}

var sendMessageMark = []byte(`"name":"SendMessage"`)

// parseLine folds one transcript line into st. It reports false when the line
// is not a JSON object at all.
func parseLine(line []byte, st *tstate) bool {
	var r rawLine
	if err := json.Unmarshal(line, &r); err != nil {
		var te *json.UnmarshalTypeError
		if !errors.As(err, &te) {
			return false
		}
	}
	if r.Cwd != "" {
		st.cwd = clip(r.Cwd, 512)
	}
	if r.GitBranch != "" {
		st.branch = clip(r.GitBranch, 128)
	}
	if r.Slug != "" {
		st.slug = clip(r.Slug, 128)
	}
	switch r.Type {
	case "ai-title":
		if r.AITitle != "" {
			st.title = clip(r.AITitle, 200)
		}
	case "custom-title":
		if r.CustomTitle != "" {
			st.custom = clip(r.CustomTitle, 200)
		}
	case "pr-link":
		if r.PRRepo != "" && r.PRNumber > 0 {
			st.pr = clip(r.PRRepo, 100) + "#" + strconv.Itoa(r.PRNumber)
		}
	case "assistant":
		if bytes.Contains(line, sendMessageMark) {
			var t rawTools
			_ = json.Unmarshal(line, &t) // a type error leaves what decoded
			for _, b := range t.Message.Content {
				if b.Type == "tool_use" && b.Name == "SendMessage" {
					st.sent++
					if st.sentTo == nil {
						st.sentTo = map[string]int{}
					}
					if _, ok := st.sentTo[b.Input.To]; ok || len(st.sentTo) < maxSentTo {
						st.sentTo[clip(b.Input.To, 64)]++
					}
				}
			}
		}
	}
	if r.Timestamp != "" {
		if t, err := time.Parse(time.RFC3339Nano, r.Timestamp); err == nil && t.After(st.lastTS) {
			st.lastTS = t
		}
	}
	return true
}

// clip bounds a string taken from untrusted input to n bytes, on a rune
// boundary.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// scan reads the lines of f from st.offset and folds them into st. It returns
// the number of bytes consumed from the file.
func scan(f *os.File, size int64, st *tstate, tail int64, maxLine int) (read int64, err error) {
	if st.size > size { // the file shrank: it was rewritten, so start over
		*st = tstate{}
	}
	st.size = size
	start := st.offset
	dropFirst := false
	if start == 0 && tail > 0 && size > tail {
		start = size - tail
		dropFirst = true // the first line of the window is cut in the middle
	}
	if start > size {
		start = size
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return 0, err
	}
	br := bufio.NewReaderSize(f, 64*1024)
	pos := start
	if dropFirst {
		n, err := discardLine(br)
		pos += n
		if err != nil {
			st.offset = pos
			return pos - start, nil
		}
	}
	for {
		line, n, complete, err := readLine(br, maxLine)
		if !complete {
			// Either EOF in the middle of a line (left for the next pass) or a
			// read error.
			if err != nil && !errors.Is(err, io.EOF) {
				st.offset = pos
				return pos - start, err
			}
			break
		}
		pos += n
		switch {
		case line == nil:
			st.skipped++
		case len(bytes.TrimSpace(line)) == 0:
		default:
			if !parseLine(line, st) {
				st.bad++
			}
		}
	}
	st.offset = pos
	return pos - start, nil
}

// discardLine drops input up to and including the next newline.
func discardLine(br *bufio.Reader) (int64, error) {
	var n int64
	for {
		b, err := br.ReadSlice('\n')
		n += int64(len(b))
		if err == nil {
			return n, nil
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return n, err
		}
	}
}

// readLine returns the next newline-terminated line. A line over maxLine is
// consumed and returned as nil with complete true. complete is false when the
// input ends before a newline, in which case nothing was consumed for the
// caller's purposes (the offset must not advance past it).
func readLine(br *bufio.Reader, maxLine int) (line []byte, n int64, complete bool, err error) {
	var buf []byte
	over := false
	for {
		b, e := br.ReadSlice('\n')
		n += int64(len(b))
		if !over {
			if len(buf)+len(b) > maxLine {
				over = true
				buf = nil
			} else {
				buf = append(buf, b...)
			}
		}
		switch {
		case e == nil:
			if over {
				return nil, n, true, nil
			}
			return bytes.TrimRight(buf, "\r\n"), n, true, nil
		case errors.Is(e, bufio.ErrBufferFull):
			continue
		default:
			return nil, 0, false, e
		}
	}
}
