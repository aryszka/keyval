package keyval

import (
	"errors"
	"io"
)

const DefaultReadBufferSize = 1 << 18

type readEntry struct {
	comment []byte
	section [][]byte
	key     [][]byte
	val     []byte
}

type Reader struct {
	BufferSize     int
	reader         io.Reader
	buffer         []byte
	state          readState
	entries        []*readEntry
	escape         bool
	escapeNext     bool
	comment        []byte
	section        [][]byte
	currentSection []byte
	key            [][]byte
	currentKey     []byte
	val            []byte
	whitespace     []byte
	commentApplied bool
	sectionApplied bool
	err            error
}

var EOFIncomplete = errors.New("EOF: incomplete data")

func escape(c byte) bool       { return c == EscapeChar }
func startComment(c byte) bool { return c == CommentChar }
func openSection(c byte) bool  { return c == OpenSectionChar }
func closeSection(c byte) bool { return c == CloseSectionChar }
func startValue(c byte) bool   { return c == StartValueChar }
func keySeparator(c byte) bool { return c == KeySeparatorChar }
func whitespace(c byte) bool   { return c == SpaceChar || c == TabChar }
func newline(c byte) bool      { return c == NewlineChar }

func NewReader(r io.Reader) *Reader {
	return &Reader{
		BufferSize: DefaultReadBufferSize,
		reader:     r,
		state:      stateInitial}
}

func (r *Reader) checkEscape(c byte) bool {
	r.escape = false
	if r.escapeNext {
		r.escape = true
		r.escapeNext = false
	} else if escape(c) {
		r.escapeNext = true
	}

	return r.escapeNext
}

func (r *Reader) appendWhitespace(c byte) { r.whitespace = append(r.whitespace, c) }
func (r *Reader) clearWhitespace()        { r.whitespace = nil }

func (r *Reader) clearComment()      { r.comment = nil }
func (r *Reader) commentWhitespace() { r.comment = append(r.comment, r.whitespace...) }

func (r *Reader) appendComment(c byte) {
	r.comment = append(r.comment, c)
	r.commentApplied = false
}

func (r *Reader) clearSection() {
	if !r.sectionApplied && len(r.section) > 0 {
		r.completeEntry()
	}

	r.section = nil
}

func (r *Reader) appendSection(c byte) { r.currentSection = append(r.currentSection, c) }
func (r *Reader) sectionWhitespace()   { r.currentSection = append(r.currentSection, r.whitespace...) }

func (r *Reader) completeSection() {
	if len(r.currentSection) > 0 {
		r.section = append(r.section, r.currentSection)
	}

	r.currentSection = nil
	r.sectionApplied = false
}

func (r *Reader) appendKey(c byte) { r.currentKey = append(r.currentKey, c) }

func (r *Reader) completeKey() {
	r.key = append(r.key, r.currentKey)
	r.currentKey = nil
}

func (r *Reader) keyWhitespace() {
	if len(r.currentKey) > 0 {
		r.currentKey = append(r.currentKey, r.whitespace...)
	}
}

func (r *Reader) appendValue(c byte) { r.val = append(r.val, c) }
func (r *Reader) valueWhitespace()   { r.val = append(r.val, r.whitespace...) }

func (r *Reader) completeEntry() {
	r.entries = append(r.entries, &readEntry{
		comment: r.comment,
		section: r.section,
		key:     r.key,
		val:     r.val})
	r.key = nil
	r.val = nil
	r.commentApplied = true
	r.sectionApplied = true
}

func mergeKey(section, key [][]byte) []string {
	skey := make([]string, len(section)+len(key))
	for i, k := range append(section, key...) {
		skey[i] = string(k)
	}

	return skey
}

func (r *Reader) fetchEntry() *Entry {
	if len(r.entries) == 0 {
		return nil
	}

	var next *readEntry
	next, r.entries = r.entries[0], r.entries[1:]

	return &Entry{
		Key:     mergeKey(next.section, next.key),
		Val:     string(next.val),
		Comment: string(next.comment)}
}

func (r *Reader) hasRemainderSection() bool {
	switch r.state {
	case
		stateSectionInitial,
		stateSection,
		stateSectionOrElse:
		return true
	default:
		return false
	}
}

func (r *Reader) hasIncompleteEntry() bool {
	return len(r.currentKey) > 0 ||
		len(r.key) > 0 ||
		len(r.val) > 0 ||
		(!r.commentApplied && len(r.comment) > 0) ||
		(!r.sectionApplied && len(r.section) > 0)
}

func (r *Reader) eofResult() (*Entry, error) {
	err := io.EOF
	hrs := r.hasRemainderSection()
	if hrs || r.escapeNext {
		err = EOFIncomplete
	}

	var last *Entry
	if r.hasIncompleteEntry() {
		if len(r.currentKey) > 0 {
			r.completeKey()
		}

		r.completeEntry()
		last = r.fetchEntry()
	}

	return last, err
}

func (r *Reader) ReadEntry() (*Entry, error) {
	if r.reader == nil {
		return nil, nil
	}

	next := r.fetchEntry()
	if next != nil {
		return next, nil
	}

	if r.err == io.EOF {
		return r.eofResult()
	}

	if len(r.buffer) != r.BufferSize {
		bsize := r.BufferSize
		if bsize <= 0 {
			bsize = 1
		}

		r.buffer = make([]byte, bsize)
	}

	if r.err != nil {
		return nil, r.err
	}

	for {
		var l int
		l, r.err = r.reader.Read(r.buffer)
		if r.err != nil && r.err != io.EOF {
			return nil, r.err
		}

		if r.err == io.EOF && l == 0 {
			return r.eofResult()
		}

		for i := 0; i < l; i++ {
			c := r.buffer[i]

			if r.checkEscape(c) {
				continue
			}

			r.appendChar(c)
		}

		next = r.fetchEntry()
		if next != nil {
			return next, nil
		}

		if r.err == io.EOF {
			return r.eofResult()
		}

		if l == 0 {
			return nil, nil
		}
	}
}
