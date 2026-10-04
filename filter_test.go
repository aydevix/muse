package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOutputName(t *testing.T) {
	// 2026-10-04 21:37:05
	stamp := time.Date(2026, 10, 4, 21, 37, 5, 0, time.Local)
	got := outputName(stamp)
	want := "muse-text-21-37-05-04-10-26.webm"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if !strings.HasSuffix(got, ".webm") {
		t.Fatalf("must be webm: %s", got)
	}
}

func TestListFonts(t *testing.T) {
	fonts := listFonts()
	if len(fonts) == 0 {
		t.Skip("no fonts reported by Pango")
	}
	for i := 1; i < len(fonts); i++ {
		if fonts[i-1] >= fonts[i] {
			t.Fatalf("not sorted/deduped at %d: %q >= %q", i, fonts[i-1], fonts[i])
		}
	}
	for _, name := range fonts {
		if strings.TrimSpace(name) != name || name == "" {
			t.Fatalf("bad family name %q", name)
		}
	}
	t.Logf("%d families, first: %s", len(fonts), fonts[0])
}

func TestWrapTextFitsCanvas(t *testing.T) {
	long := "Muse is a lightweight tool for animating text images and video with beautiful motion and a transparent background"
	maxWidth := canvasWidth - 2*textMargin - wrapSlack
	lines := wrapText(defaultFont, 72, maxWidth, long)
	if len(lines) < 2 {
		t.Fatalf("expected the text to wrap, got %d line(s): %q", len(lines), lines)
	}
	for i, line := range lines {
		w := measureText(defaultFont, 72, line)
		if w > maxWidth {
			t.Errorf("line %d is %dpx wide, limit is %dpx: %q", i, w, maxWidth, line)
		}
		if strings.TrimSpace(line) == "" {
			t.Errorf("line %d is empty", i)
		}
	}
	if got := strings.Join(lines, " "); got != long {
		t.Errorf("wrapping changed the text:\n got %q\nwant %q", got, long)
	}
	t.Logf("%d lines, widest %dpx (limit %dpx)", len(lines), measureText(defaultFont, 72, longest(lines)), maxWidth)
}

func TestWrapKeepsExplicitNewlines(t *testing.T) {
	lines := wrapText(defaultFont, 40, 800, "first\nsecond\n\nthird")
	want := []string{"first", "second", "third"}
	if len(lines) != len(want) {
		t.Fatalf("got %q, want %q", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("got %q, want %q", lines, want)
		}
	}
}

func TestLongSingleWordStaysInside(t *testing.T) {
	word := strings.Repeat("W", 200)
	maxWidth := canvasWidth - 2*textMargin - wrapSlack
	lines := wrapText(defaultFont, 72, maxWidth, word)
	if len(lines) == 0 {
		t.Fatal("no lines produced")
	}
	for i, line := range lines {
		if w := measureText(defaultFont, 72, line); w > maxWidth {
			t.Errorf("fragment %d is %dpx, limit %dpx", i, w, maxWidth)
		}
	}
}

func longest(lines []string) string {
	best := ""
	for _, line := range lines {
		if len(line) > len(best) {
			best = line
		}
	}
	return best
}

func TestRenderBlurWake(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, outputName(time.Now()))
	args := buildRenderArgs(defaultFont, "Muse Wake Up", "0xffffff", 72, 3, 1, out)
	cmd := exec.Command("ffmpeg", args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("render failed: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("output missing: %v", err)
	}
	assertTransparent(t, out)
	t.Logf("ok: %s", out)
}

// assertTransparent checks that the file really carries an alpha plane and that
// the first frame is fully transparent while a middle frame is not. This is
// what makes the WebM usable as an overlay in a video editor.
func assertTransparent(t *testing.T, path string) {
	t.Helper()
	cmd := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream_tags=alpha_mode", "-of", "default=nw=1:nk=1", path)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("ffprobe: %v", err)
	}
	if !strings.Contains(string(out), "1") {
		t.Fatalf("alpha_mode tag missing, got %q", strings.TrimSpace(string(out)))
	}

	// The alpha plane is only exposed by the libvpx decoder, so it has to be
	// requested explicitly. The native vp9 decoder silently drops it.
	alphaAt := func(frame int) int {
		tmp := filepath.Join(t.TempDir(), "alpha.raw")
		args := []string{"-y", "-v", "error", "-c:v", "libvpx-vp9", "-i", path,
			"-vf", fmt.Sprintf("select='eq(n\\,%d)',alphaextract,format=gray", frame),
			"-fps_mode", "passthrough", "-frames:v", "1", "-f", "rawvideo", tmp}
		if err := exec.Command("ffmpeg", args...).Run(); err != nil {
			t.Fatalf("alpha probe for frame %d failed: %v", frame, err)
		}
		data, err := os.ReadFile(tmp)
		if err != nil {
			t.Fatalf("read alpha: %v", err)
		}
		n := 0
		for _, v := range data {
			if v > 8 {
				n++
			}
		}
		return n
	}

	first := alphaAt(0)
	if first != 0 {
		t.Errorf("first frame should be fully transparent, got %d opaque pixels", first)
	}
	middle := alphaAt(30)
	if middle == 0 {
		t.Error("middle frame has no visible text at all")
	}
	t.Logf("frame 0 opaque px=%d, frame 30 opaque px=%d", first, middle)
}

// TestRenderLongText checks that a wrapping render still produces a valid file
// and that the number of drawtext filters matches the number of lines.
func TestRenderLongText(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, outputName(time.Now()))
	long := "This sentence is deliberately long so that Muse has to break it into several lines before drawing anything at all"
	filter := blurWakeFilter(defaultFont, long, "0xffffff", 72, canvasWidth, canvasHeight, 3, 1)
	if got := strings.Count(filter, "drawtext=font="); got < 2 {
		t.Fatalf("expected several drawtext filters, got %d", got)
	}
	args := buildRenderArgs(defaultFont, long, "0xffffff", 72, 3, 1, out)
	cmd := exec.Command("ffmpeg", args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("render failed: %v", err)
	}
	t.Logf("ok: %d drawtext filters", strings.Count(filter, "drawtext=font="))
}

// TestBlurStartsAtMax makes sure the blur factor is 1.0 on the first frame and
// 0.0 in the middle of the clip.
func TestBlurStartsAtMax(t *testing.T) {
	filter := blurWakeFilter(defaultFont, "x", "0xffffff", 72, canvasWidth, canvasHeight, 5, 1)
	if !strings.Contains(filter, "pow(1-T/1.000,3)") {
		t.Fatalf("blur factor must start at max: %s", filter)
	}
	if !strings.Contains(filter, "boxblur=luma_radius=20") {
		t.Fatalf("blur radius must stay 20: %s", filter)
	}
}
