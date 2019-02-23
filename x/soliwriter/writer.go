package soliwriter

import "net/http"

// InterceptResponseWriter 接管404
type InterceptResponseWriter struct {
	http.ResponseWriter
	ErrH func(http.ResponseWriter, int)
}

// WriteHeader 写HTTP头
func (w InterceptResponseWriter) WriteHeader(status int) {
	if status == http.StatusNotFound {
		w.ErrH(w.ResponseWriter, status)
		w.ErrH = nil
	} else {
		w.ResponseWriter.WriteHeader(status)
	}
}

func (w InterceptResponseWriter) Write(p []byte) (n int, err error) {
	if w.ErrH != nil {
		return len(p), nil
	}
	return w.ResponseWriter.Write(p)
}
