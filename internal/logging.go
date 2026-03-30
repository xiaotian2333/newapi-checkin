package app

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"sync"
	"time"
)

var utcPlus8 = time.FixedZone("UTC+8", 8*60*60)

type logWriter struct {
	out io.Writer
	mu  sync.Mutex
	buf []byte
}

func configureLogging(out io.Writer) {
	log.SetFlags(0)
	log.SetOutput(&logWriter{out: out})
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.buf = append(w.buf, p...)
	for {
		index := bytes.IndexByte(w.buf, '\n')
		if index < 0 {
			break
		}

		line := w.buf[:index]
		if _, err := fmt.Fprintf(w.out, "%s %s\n", time.Now().In(utcPlus8).Format("2006/01/02 15:04:05"), line); err != nil {
			return 0, err
		}

		w.buf = w.buf[index+1:]
	}

	return len(p), nil
}
