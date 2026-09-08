package janet

import (
	"net/url"
	"path/filepath"
	"sort"
	"unicode/utf8"
)

type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

func (document *Document) Offset(position Position) int {
	if len(document.LineStarts) == 0 {
		return 0
	}
	line := max(0, min(position.Line, len(document.LineStarts)-1))
	start := document.LineStarts[line]
	end := len(document.Source)
	if line+1 < len(document.LineStarts) {
		end = document.LineStarts[line+1] - 1
	}

	offset := start
	units := 0
	for offset < end && units < position.Character {
		r, size := utf8.DecodeRuneInString(document.Source[offset:end])
		width := 1
		if r > 0xffff {
			width = 2
		}
		if units+width > position.Character {
			break
		}
		units += width
		offset += size
	}
	return offset
}

func (document *Document) Position(offset int) Position {
	offset = max(0, min(offset, len(document.Source)))
	line := sort.Search(len(document.LineStarts), func(index int) bool {
		return document.LineStarts[index] > offset
	}) - 1
	line = max(line, 0)
	character := 0
	for index := document.LineStarts[line]; index < offset; {
		r, size := utf8.DecodeRuneInString(document.Source[index:offset])
		character++
		if r > 0xffff {
			character++
		}
		index += size
	}
	return Position{Line: line, Character: character}
}

func (document *Document) Range(start, end int) Range {
	return Range{Start: document.Position(start), End: document.Position(end)}
}

func PathToURI(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		absolute = path
	}
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(absolute)}).String()
}

func URIToPath(uri string) (string, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" {
		return filepath.Abs(uri)
	}
	return filepath.FromSlash(parsed.Path), nil
}
