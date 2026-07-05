package remote

import (
	"time"

	"log/slog"
)

// resilientSource kapselt eine screenSource und stellt sie bei Aufnahme-Fehlern neu
// her – z.B. bei Ab-/Anmelden (Session-Wechsel), wenn der Nutzer-Session-Helfer
// stirbt. So läuft die Fernsteuerung ohne Neuverbinden weiter, sobald wieder eine
// Sitzung da ist. Die Framebuffer-Größe bleibt fix (Auflösungswechsel: später via
// DesktopSize-Pseudo-Encoding).
type resilientSource struct {
	log     *slog.Logger
	monitor int
	inner   screenSource
	w, h    int
}

func newResilientSource(log *slog.Logger, monitor int) (*resilientSource, error) {
	s, err := newScreenSource(log, monitor)
	if err != nil {
		return nil, err
	}
	w, h := s.Bounds()
	return &resilientSource{log: log, monitor: monitor, inner: s, w: w, h: h}, nil
}

func (r *resilientSource) Bounds() (int, int) { return r.w, r.h }

func (r *resilientSource) Capture() ([]byte, error) {
	if r.inner == nil {
		s, err := newScreenSource(r.log, r.monitor)
		if err != nil {
			return nil, err // noch keine Sitzung -> Aufrufer sendet leeres Update
		}
		r.inner = s
	}
	px, err := r.inner.Capture()
	if err != nil {
		r.log.Info("aufnahme unterbrochen – stelle neu her (z.B. Ab-/Anmelden)", "err", err)
		r.inner.Close()
		r.inner = nil
		return nil, err
	}
	return px, nil
}

func (r *resilientSource) Pointer(mask, x, y int) {
	if in, ok := r.inner.(inputSink); ok {
		in.Pointer(mask, x, y)
	}
}

func (r *resilientSource) Key(down bool, keysym uint32) {
	if in, ok := r.inner.(inputSink); ok {
		in.Key(down, keysym)
	}
}

func (r *resilientSource) GetClipboard() (string, bool) {
	if cs, ok := r.inner.(clipboardSource); ok {
		return cs.GetClipboard()
	}
	return "", false
}

func (r *resilientSource) SetClipboard(text string) {
	if cs, ok := r.inner.(clipboardSource); ok {
		cs.SetClipboard(text)
	}
}

func (r *resilientSource) Close() error {
	if r.inner != nil {
		return r.inner.Close()
	}
	return nil
}

// syntheticSource erzeugt ein bewegtes Testbild. Dient zum Verifizieren des
// RFB-Servers/Tunnels und als Übergangs-Quelle auf Plattformen ohne echte Aufnahme.
type syntheticSource struct {
	w, h int
	buf  []byte
}

func newSyntheticSource() *syntheticSource {
	s := &syntheticSource{w: 1280, h: 720}
	s.buf = make([]byte, s.w*s.h*4)
	return s
}

func (s *syntheticSource) Bounds() (int, int) { return s.w, s.h }
func (s *syntheticSource) Close() error       { return nil }

func (s *syntheticSource) Capture() ([]byte, error) {
	t := int(time.Now().UnixMilli() / 40)
	for y := 0; y < s.h; y++ {
		row := y * s.w * 4
		for x := 0; x < s.w; x++ {
			i := row + x*4
			s.buf[i+0] = byte(x + t) // B
			s.buf[i+1] = byte(y + t) // G
			s.buf[i+2] = byte(x ^ y) // R
			s.buf[i+3] = 0
		}
	}
	return s.buf, nil
}
