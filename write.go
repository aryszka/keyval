package keyval

import (
	"errors"
	"io"
	"strings"
)

type EntryWriter struct {
	MaxSectionDepth int
	MinKeyDepth     int
	KnownSections   [][]string
	writer          io.Writer
	started         bool
	comment         string
	inComment       bool
	section         []string
	err             error
}

var ErrWriteLength = errors.New("write failed: byte count does not match")

func NewEntryWriter(w io.Writer) *EntryWriter {
	return &EntryWriter{writer: w, MaxSectionDepth: 1, MinKeyDepth: 1}
}

func escapeWrite(b, ec []byte) []byte {
	eb := make([]byte, 0, len(b))
	en := 0
	for i, c := range b {
		for _, e := range ec {
			if c == e {
				eb = append(eb, b[len(eb)-en:i]...)
				eb = append(eb, EscapeChar, c)
				en++
			}
		}
	}

	eb = append(eb, b[len(eb)-en:]...)
	return eb
}

func escapeBoundaries(b, lec, tec []byte) []byte {
	switch {
	case len(b) == 0:
		return b
	case len(b) == 1:
		return escapeWrite(b, lec)
	default:
		return append(escapeWrite(b[:1], lec),
			append(b[1:len(b)-1],
				escapeWrite(b[len(b)-1:], tec)...)...)
	}
}

func escapeOutput(b, wec, lec, tec []byte) []byte {
	return escapeBoundaries(escapeWrite(b, wec), lec, tec)
}

func (w *EntryWriter) write(b ...byte) error {
	if l, err := w.writer.Write(b); err != nil {
		return err
	} else if l != len(b) {
		return ErrWriteLength
	}

	return nil
}

func (w *EntryWriter) writeLine() error {
	return w.write(NewlineChar)
}

func (w *EntryWriter) commentChanged(comment string) bool {
	return w.comment != comment
}

func (w *EntryWriter) writeComment() error {
	var err error

	withError := func(f ...func() error) {
		for err == nil && len(f) > 0 {
			err = f[0]()
			f = f[1:]
		}
	}

	writeWithError := func(b ...byte) {
		withError(func() error { return w.write(b...) })
	}

	if w.inComment {
		withError(w.writeSection)
		if len(w.comment) == 0 {
			writeWithError(SpaceChar)
		} else {
			writeWithError(NewlineChar, NewlineChar)
		}
	}

	if w.comment == "" {
		writeWithError(CommentChar)
	}

	lines := strings.Split(w.comment, string([]byte{NewlineChar}))
	for _, l := range lines {
		if len(l) == 0 {
			writeWithError(CommentChar, NewlineChar)
		} else {
			writeWithError(append([]byte{CommentChar, SpaceChar},
				append(escapeOutput([]byte(l), escapeComment, escapeBoundComment, escapeBound),
					NewlineChar)...)...)
		}
	}

	return err
}

func (w *EntryWriter) splitKey(key []string) ([]string, []string) {
	if len(key) == 0 {
		return nil, nil
	}

	for _, ks := range w.KnownSections {
		if len(ks) > len(key) {
			continue
		}

		known := true
		for i, sn := range ks {
			if sn != key[i] {
				known = false
				break
			}
		}

		if known {
			return ks, key[len(ks):]
		}
	}

	if len(key) < w.MinKeyDepth {
		return nil, key
	}

	sectionDepth := w.MaxSectionDepth
	if len(key)-sectionDepth < w.MinKeyDepth {
		sectionDepth = len(key) - w.MinKeyDepth
	}

	return key[:sectionDepth], key[sectionDepth:]
}

func (w *EntryWriter) sectionChanged(section []string) bool {
	if len(section) != len(w.section) {
		return true
	}

	for i, sn := range section {
		if sn != w.section[i] {
			return true
		}
	}

	return false
}

func (w *EntryWriter) writeKeyEscaped(key []string, wesc, besc []byte) error {
	first := true
	for _, s := range key {
		if !first {
			if err := w.write(KeySeparatorChar); err != nil {
				return err
			}
		}

		if err := w.write(escapeOutput([]byte(s), wesc, besc, besc)...); err != nil {
			return err
		}

		first = false
	}

	return nil
}

func (w *EntryWriter) writeSection() error {
	if err := w.write(OpenSectionChar); err != nil {
		return err
	}

	if err := w.writeKeyEscaped(w.section, escapeSection, escapeBoundNl); err != nil {
		return err
	}

	return w.write(CloseSectionChar)
}

func (w *EntryWriter) writeKey(key []string) error {
	return w.writeKeyEscaped(key, escapeKey, escapeBound)
}

func (w *EntryWriter) writeVal(val string, leadingSpace bool) error {
	if leadingSpace {
		if err := w.write(SpaceChar); err != nil {
			return err
		}
	}

	if err := w.write(StartValueChar, SpaceChar); err != nil {
		return err
	}

	return w.write(escapeOutput([]byte(val), escapeVal, escapeBound, escapeBound)...)
}

func (w *EntryWriter) WriteEntry(e *Entry) error {
	withError := func(f ...func() error) {
		for w.err == nil && len(f) > 0 {
			w.err = f[0]()
			f = f[1:]
		}
	}

	var (
		commentWritten bool
		sectionWritten bool
		keyWritten     bool
		valWritten     bool
	)

	if w.writer == nil || e == nil || w.err != nil {
		return w.err
	}

	if w.commentChanged(e.Comment) {
		w.comment = e.Comment

		if w.started {
			withError(w.writeLine)
		}

		withError(w.writeComment)
		commentWritten = true
		w.inComment = true
	}

	section, key := w.splitKey(e.Key)
	if w.sectionChanged(section) && (len(key) != 0 || len(e.Val) != 0) {
		w.section = section

		if w.started && !commentWritten {
			withError(w.writeLine)
		}

		withError(w.writeSection, w.writeLine)
		sectionWritten = true
	}

	if len(key) != 0 {
		withError(func() error { return w.writeKey(key) })
		keyWritten = true
	}

	if len(e.Val) > 0 {
		withError(func() error { return w.writeVal(e.Val, keyWritten) })
		valWritten = true
	}

	if keyWritten || valWritten {
		withError(w.writeLine)
	}

	w.started = w.started || commentWritten || sectionWritten || keyWritten || valWritten
	w.inComment = w.inComment && !sectionWritten && !keyWritten && !valWritten
	return w.err
}
