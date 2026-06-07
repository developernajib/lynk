package edge

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"sync"
)

// Gzip compresses eligible text responses for clients that ask for it.
// gRPC-Web traffic is deliberately skipped: its framing has per-message
// boundaries and flushing that a buffering gzip stream would delay or
// corrupt, and gRPC negotiates its own per-message compression at the right
// layer. Writers are pooled because each one allocates ~256 KB of buffers;
// recycling them keeps the hot path allocation-free.
func Gzip() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
				next.ServeHTTP(w, r)
				return
			}
			if strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc-web") {
				next.ServeHTTP(w, r)
				return
			}

			// Vary keys caches on Accept-Encoding so a gzipped body is never
			// served to a client that cannot decode it. Set unconditionally
			// for consistent cache behavior.
			w.Header().Add("Vary", "Accept-Encoding")

			gw := &gzipResponseWriter{ResponseWriter: w}
			// The deferred close flushes the gzip trailer; without it the
			// client receives a truncated stream.
			defer gw.close()

			next.ServeHTTP(gw, r)
		})
	}
}

// Level 6 (default): levels above cost ~2x CPU for single-digit extra savings.
var gzipWriterPool = sync.Pool{
	New: func() any {
		w, _ := gzip.NewWriterLevel(io.Discard, gzip.DefaultCompression)
		return w
	},
}

// An allowlist is the safe default: unknown types pass uncompressed, which is
// always correct, just not optimal.
var compressibleTypes = []string{
	"application/json",
	"application/javascript",
	"application/xml",
	"image/svg+xml",
	"text/",
}

// gzipResponseWriter decides at WriteHeader time whether to compress, since
// only then is the Content-Type known.
type gzipResponseWriter struct {
	http.ResponseWriter
	gz          *gzip.Writer // nil until we decide to compress
	wroteHeader bool
}

func (w *gzipResponseWriter) WriteHeader(code int) {
	if w.wroteHeader {
		w.ResponseWriter.WriteHeader(code)
		return
	}
	w.wroteHeader = true

	h := w.Header()
	if h.Get("Content-Encoding") == "" && isCompressible(h.Get("Content-Type")) {
		h.Set("Content-Encoding", "gzip")
		// The compressed length is unknown; keeping Content-Length would cut
		// the body short. Deleting it switches to chunked transfer.
		h.Del("Content-Length")

		gz := gzipWriterPool.Get().(*gzip.Writer) //nolint:errcheck // pool stores exactly *gzip.Writer
		gz.Reset(w.ResponseWriter)
		w.gz = gz
	}

	w.ResponseWriter.WriteHeader(code)
}

func (w *gzipResponseWriter) Write(data []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if w.gz != nil {
		return w.gz.Write(data)
	}
	return w.ResponseWriter.Write(data)
}

// Flush keeps the wrapper transparent to streaming handlers that
// type-assert http.Flusher.
func (w *gzipResponseWriter) Flush() {
	if w.gz != nil {
		_ = w.gz.Flush()
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// close finalizes the stream and recycles the writer. Resetting onto
// io.Discard first means a pooled writer never pins a finished response.
func (w *gzipResponseWriter) close() {
	if w.gz == nil {
		return
	}
	_ = w.gz.Close()
	w.gz.Reset(io.Discard)
	gzipWriterPool.Put(w.gz)
	w.gz = nil
}

func isCompressible(contentType string) bool {
	for _, prefix := range compressibleTypes {
		if strings.HasPrefix(contentType, prefix) {
			return true
		}
	}
	return false
}
