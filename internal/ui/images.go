package ui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"math"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattermost/mattermost/server/public/model"
	"golang.org/x/image/draw"

	"github.com/codihaus/mmt/internal/launcher"
)

// iTerm2 draws images sent with its inline-image escape (OSC 1337).
// Multiplexers swallow the escape, so images are only used in a direct
// session; MMT_NO_IMAGES=1 turns them off.
var inlineImages = (os.Getenv("TERM_PROGRAM") == "iTerm.app" || os.Getenv("LC_TERMINAL") == "iTerm2") &&
	!launcher.InMultiplexer() && os.Getenv("MMT_NO_IMAGES") == ""

const (
	imgMaxCols = 48
	imgMaxRows = 14
	imgMaxPx   = 720 // downscale before sending; keeps each redraw small
)

type imgState struct {
	b64     string
	loading bool
	failed  bool
}

// imgRef marks the first placeholder row of an image inside a block.
type imgRef struct {
	id         string
	cols, rows int
}

// imgPlace is an image's position in the viewport content.
type imgPlace struct {
	line, col  int
	cols, rows int
	id         string
}

type imgMsg struct {
	id  string
	b64 string
	err error
}

// imgCells picks a cell box for the image, assuming cells are about twice
// as tall as they are wide.
func imgCells(f *model.FileInfo, maxCols int) (cols, rows int) {
	w, h := float64(f.Width), float64(f.Height)
	if w <= 0 || h <= 0 {
		w, h = 4, 3
	}
	cols = min(maxCols, imgMaxCols)
	rows = int(math.Round(float64(cols) * h / w / 2))
	rows = min(max(rows, 2), imgMaxRows)
	cols = min(cols, max(4, int(math.Round(float64(rows)*2*w/h))))
	return cols, rows
}

// imgCacheMax bounds decoded images kept in memory; the oldest are dropped
// and fetched again if they scroll back into view.
const imgCacheMax = 200

func (m *Model) wantImage(id string) {
	if st := m.imgs[id]; st == nil {
		m.imgs[id] = &imgState{}
		m.imgOrder = append(m.imgOrder, id)
		m.imgWant = append(m.imgWant, id)
		for len(m.imgOrder) > imgCacheMax {
			delete(m.imgs, m.imgOrder[0])
			m.imgOrder = m.imgOrder[1:]
		}
	}
}

func (m *Model) imageCmds() tea.Cmd {
	if len(m.imgWant) == 0 {
		return nil
	}
	var cmds []tea.Cmd
	for _, id := range m.imgWant {
		m.imgs[id].loading = true
		cmds = append(cmds, m.imageCmd(id))
	}
	m.imgWant = nil
	return tea.Batch(cmds...)
}

// imageCmd downloads the server-side preview, shrinks it and re-encodes it
// as JPEG so each draw sends tens of KB rather than megabytes.
func (m *Model) imageCmd(id string) tea.Cmd {
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		data, _, err := m.c.API.GetFilePreview(cx, id)
		if err != nil {
			return imgMsg{id: id, err: err}
		}
		src, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return imgMsg{id: id, err: err}
		}
		b := src.Bounds()
		w, h := b.Dx(), b.Dy()
		if w > imgMaxPx {
			h = h * imgMaxPx / w
			w = imgMaxPx
		}
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		// white under transparent pixels, JPEG has no alpha
		draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
		draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 82}); err != nil {
			return imgMsg{id: id, err: err}
		}
		return imgMsg{id: id, b64: base64.StdEncoding.EncodeToString(buf.Bytes())}
	}
}

func (m *Model) onImage(msg imgMsg) {
	st := m.imgs[msg.id]
	if st == nil {
		return
	}
	st.loading = false
	if msg.err != nil {
		st.failed = true
		return
	}
	st.b64 = msg.b64
}

// imageOverlay returns escapes that draw every fully visible image at its
// screen position. It is appended to the last screen line, which the
// renderer writes last, so the images land on top of the placeholders. A
// hash of the rows under the images is included so the line (and with it the
// images) is only re-sent when those rows change.
func (m *Model) imageOverlay(g geo, screen []string) string {
	if !inlineImages || m.sw != nil || m.showHelp {
		return ""
	}
	var esc strings.Builder
	sig := fnv.New64a()
	for k, pg := range g.panes {
		if pg.w == 0 {
			continue
		}
		pn := m.panes[k]
		top := pg.vpTop + m.padTop(paneKind(k))
		for _, pl := range pn.imgs {
			st := m.imgs[pl.id]
			if st == nil || st.b64 == "" {
				continue
			}
			r := pl.line - pn.vp.YOffset
			if r < 0 || r+pl.rows > pn.vp.Height {
				continue
			}
			row, col := top+r, g.sideW+pg.x+pl.col
			fmt.Fprintf(&esc, "\x1b7\x1b[%d;%dH\x1b]1337;File=inline=1;width=%d;height=%d;preserveAspectRatio=1:%s\a\x1b8",
				row+1, col+1, pl.cols, pl.rows, st.b64)
			fmt.Fprintf(sig, "%s|%d|%d|", pl.id, row, col)
			for i := row; i < row+pl.rows && i < len(screen); i++ {
				sig.Write([]byte(screen[i]))
			}
		}
	}
	if esc.Len() == 0 {
		return ""
	}
	// SetUserVar is a harmless iTerm2 escape; it only makes the line unique
	return fmt.Sprintf("\x1b]1337;SetUserVar=mmtframe=%s\a", base64.StdEncoding.EncodeToString(sig.Sum(nil))) + esc.String()
}
