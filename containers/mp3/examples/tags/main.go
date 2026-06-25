// Tags example: read the ID3 metadata bracketing an MP3 file with
// containers/mp3 and project it onto the toolkit's standard tag set. This
// teaches one concept — inspecting ID3 tags — and nothing else.
//
// NewReader parses the leading ID3v2 tag (and the trailing ID3v1 tag when the
// source is seekable, as a *os.File is) without touching the audio codec, so
// reading tags never requires the LGPL mp3lame encoder.
//
// Usage: tags <input.mp3>
package main

import (
	"log"
	"os"

	ctrmp3 "github.com/daniel-sullivan/go-mediatoolkit/containers/mp3"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("usage: %s <input.mp3>", os.Args[0])
	}

	f, err := os.Open(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	// NewReader parses the ID3 metadata and projects it onto the standard tag
	// set. The file is an io.ReadSeeker, so the trailing ID3v1 tag is folded in
	// too (ID3v2 values win on conflict).
	rd, err := ctrmp3.NewReader(f)
	if err != nil {
		log.Fatal(err)
	}

	h := rd.Header()
	log.Printf("title:  %s", deref(h.Tags.Title))
	log.Printf("artist: %s", deref(h.Tags.Artist))
	log.Printf("album:  %s", deref(h.Tags.Album))

	log.Printf("ID3v2 version: %d (has ID3v1 trailer: %t)",
		h.Extra.ID3v2Version, h.Extra.HasID3v1)
	log.Printf("non-standard frames: %d, embedded pictures: %d",
		len(h.Extra.RawFrames), len(h.Extra.Pictures))
}

// deref returns the string a tag pointer holds, or "(absent)" if nil.
func deref(s *string) string {
	if s == nil {
		return "(absent)"
	}
	return *s
}
