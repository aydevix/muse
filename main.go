package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/diamondburned/gotk4/pkg/gdkpixbuf/v2"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

const appID = "dev.muse.Animator"

const defaultFont = "DejaVu Sans"

const (
	canvasWidth  = 1280
	canvasHeight = 720
	// textMargin keeps wrapped lines away from the canvas edge.
	textMargin = 48
	// wrapSlack absorbs the small difference between Pango metrics and the
	// freetype metrics drawtext uses, so a line never touches the border.
	wrapSlack = 12
	// previewWidth is the width of the frames shown in the window.
	previewWidth = 480
)

type editor struct {
	window        *gtk.ApplicationWindow
	text          *gtk.Entry
	animation     *gtk.DropDown
	font          *gtk.DropDown
	fonts         []string
	previewBox    *gtk.Box
	preview       *gtk.Picture
	previewHint   *gtk.Label
	previewDir    string
	previewFrames []string
	previewIndex  int
	previewTimer  glib.SourceHandle
	outDir        *gtk.Label
	outputDir     string
	color         *gtk.Entry
	size          *gtk.SpinButton
	duration      *gtk.SpinButton
	speed         *gtk.SpinButton
	status        *gtk.Label
}

func main() {
	app := gtk.NewApplication(appID, gio.ApplicationFlagsNone)
	app.ConnectActivate(func() { buildUI(app) })
	app.Run(os.Args)
}

func buildUI(app *gtk.Application) {
	e := &editor{}
	e.window = gtk.NewApplicationWindow(app)
	e.window.SetTitle("Muse")
	e.window.SetDefaultSize(760, 560)

	root := gtk.NewBox(gtk.OrientationVertical, 0)
	root.SetMarginTop(28)
	root.SetMarginBottom(28)
	root.SetMarginStart(32)
	root.SetMarginEnd(32)
	e.window.SetChild(root)

	title := gtk.NewLabel("Muse")
	title.SetHAlign(gtk.AlignStart)
	title.AddCSSClass("title-1")
	root.Append(title)
	subtitle := gtk.NewLabel("Text animation rendered to a transparent WebM")
	subtitle.SetHAlign(gtk.AlignStart)
	subtitle.AddCSSClass("dim-label")
	root.Append(subtitle)

	content := gtk.NewBox(gtk.OrientationHorizontal, 28)
	content.SetVExpand(true)
	root.Append(content)

	e.previewBox = gtk.NewBox(gtk.OrientationVertical, 0)
	e.previewBox.SetSizeRequest(390, 0)
	e.previewBox.SetVExpand(true)
	e.previewBox.SetVAlign(gtk.AlignCenter)
	e.previewBox.AddCSSClass("preview")

	e.previewHint = gtk.NewLabel("Preview appears after Render")
	e.previewHint.SetHAlign(gtk.AlignCenter)
	e.previewHint.SetVAlign(gtk.AlignCenter)
	e.previewHint.SetVExpand(true)
	e.previewHint.AddCSSClass("dim-label")
	e.previewBox.Append(e.previewHint)

	e.preview = gtk.NewPicture()
	e.preview.SetVExpand(true)
	e.preview.SetHExpand(true)
	e.preview.SetHAlign(gtk.AlignCenter)
	e.preview.SetVAlign(gtk.AlignCenter)
	e.preview.SetSizeRequest(374, 210)
	e.preview.SetVisible(false)
	e.previewBox.Append(e.preview)
	content.Append(e.previewBox)

	controls := gtk.NewBox(gtk.OrientationVertical, 12)
	controls.SetHExpand(true)
	content.Append(controls)

	e.text = gtk.NewEntry()
	e.text.SetPlaceholderText("Enter your text")
	addField(controls, "Text", e.text)

	e.animation = gtk.NewDropDownFromStrings([]string{"Blur, Wake up!"})
	addField(controls, "Animation", e.animation)

	e.fonts = listFonts()
	if len(e.fonts) == 0 {
		e.fonts = []string{"Sans"}
	}
	e.font = gtk.NewDropDownFromStrings(e.fonts)
	e.font.SetEnableSearch(true)
	e.font.SetSearchMatchMode(gtk.StringFilterMatchModeSubstring)
	for i, name := range e.fonts {
		if name == defaultFont {
			e.font.SetSelected(uint(i))
			break
		}
	}
	addField(controls, "Font", e.font)

	e.color = gtk.NewEntry()
	e.color.SetText("#ffffff")
	e.color.SetPlaceholderText("#RRGGBB")
	addField(controls, "Text color", e.color)

	e.size = gtk.NewSpinButtonWithRange(8, 256, 1)
	e.size.SetValue(72)
	addField(controls, "Size", e.size)

	e.duration = gtk.NewSpinButtonWithRange(1, 30, 1)
	e.duration.SetValue(5)
	addField(controls, "Duration (sec.)", e.duration)

	e.speed = gtk.NewSpinButtonWithRange(0.1, 10, 0.1)
	e.speed.SetValue(1)
	addField(controls, "Animation speed (sec.)", e.speed)

	render := gtk.NewButtonWithLabel("Render")
	render.AddCSSClass("suggested-action")
	render.SetMarginTop(12)
	render.ConnectClicked(func() { e.render() })
	controls.Append(render)

	// Renders must not depend on the working directory: a launcher started
	// from a menu may have anything as its cwd, including read-only paths.
	e.outputDir = defaultOutputDir()
	saveAs := gtk.NewButtonWithLabel("Save as…")
	saveAs.SetHAlign(gtk.AlignStart)
	saveAs.ConnectClicked(func() { e.chooseOutputDir() })
	controls.Append(saveAs)

	e.outDir = gtk.NewLabel("Saves to: " + e.outputDir)
	e.outDir.SetHAlign(gtk.AlignStart)
	e.outDir.SetEllipsize(3)
	e.outDir.AddCSSClass("dim-label")
	e.outDir.SetTooltipText(e.outputDir)
	controls.Append(e.outDir)

	e.status = gtk.NewLabel("Ready")
	e.status.SetHAlign(gtk.AlignStart)
	e.status.AddCSSClass("dim-label")
	root.Append(e.status)

	e.window.Present()
}

func addField(parent *gtk.Box, label string, widget gtk.Widgetter) {
	box := gtk.NewBox(gtk.OrientationVertical, 5)
	l := gtk.NewLabel(label)
	l.SetHAlign(gtk.AlignStart)
	l.AddCSSClass("dim-label")
	box.Append(l)
	box.Append(widget)
	parent.Append(box)
}

// defaultOutputDir resolves the folder renders land in. The XDG user
// directories are honoured so a user who moved ~/Videos keeps that choice.
// $HOME is the last resort, because the working directory is not writable for
// a GUI app started from a menu.
func defaultOutputDir() string {
	home, _ := os.UserHomeDir()

	var candidates []string
	// Some desktops export the resolved path directly.
	if env := strings.TrimSpace(os.Getenv("XDG_VIDEOS_DIR")); env != "" {
		candidates = append(candidates, env)
	}
	candidates = append(candidates, xdgVideoDir(home))
	if home != "" {
		candidates = append(candidates, filepath.Join(home, "Videos"))
	}
	// An unusable candidate yields "", so the chain keeps going.
	for _, dir := range candidates {
		if resolved := usableDir(dir, ""); resolved != "" {
			return resolved
		}
	}
	if home != "" {
		return home
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}

// usableDir validates a candidate folder, expanding a leading $HOME and
// creating the directory when it is missing. fallback is returned when the
// candidate cannot be used.
func usableDir(dir, fallback string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return fallback
	}
	if home := os.Getenv("HOME"); home != "" && strings.HasPrefix(dir, "$HOME") {
		dir = filepath.Join(home, strings.TrimPrefix(dir, "$HOME"))
	}
	if strings.HasPrefix(dir, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(home, strings.TrimPrefix(dir, "~/"))
		}
	}
	if !filepath.IsAbs(dir) {
		return fallback
	}
	if info, err := os.Stat(dir); err == nil {
		if info.IsDir() {
			return dir
		}
		return fallback
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fallback
	}
	return dir
}

// xdgVideoDir asks xdg-user-dir where videos belong. The tool is tiny and part
// of xdg-user-dirs, which is installed on essentially every desktop, so there
// is no reason to reimplement its config parsing. It prints $HOME when the
// directory has not been configured yet, which is reported as not found.
func xdgVideoDir(home string) string {
	path, err := exec.LookPath("xdg-user-dir")
	if err != nil {
		return ""
	}
	out, err := exec.Command(path, "VIDEO").Output()
	if err != nil {
		return ""
	}
	dir := strings.TrimSpace(string(out))
	return normaliseVideoDir(dir, home)
}

// normaliseVideoDir maps what xdg-user-dir reported onto a usable folder. The
// tool prints the home directory, already expanded, when the video directory has
// not been configured, and that must not be mistaken for a real answer.
func normaliseVideoDir(dir, home string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" || dir == "$HOME" || (home != "" && dir == home) {
		if home == "" {
			return ""
		}
		return filepath.Join(home, "Videos")
	}
	return dir
}

// chooseOutputDir asks for a folder and keeps it for later renders.
func (e *editor) chooseOutputDir() {
	dialog := gtk.NewFileDialog()
	dialog.SetTitle("Choose a folder for renders")
	dialog.SetModal(true)
	if folder := gio.NewFileForPath(e.outputDir); folder != nil {
		dialog.SetInitialFolder(folder)
	}
	filter := gtk.NewFileFilter()
	filter.SetName("Folders")
	dialog.SetDefaultFilter(filter)

	dialog.SelectMultipleFolders(context.Background(), parentWindow(e.window), func(res gio.AsyncResulter) {
		model, err := dialog.SelectMultipleFoldersFinish(res)
		if err != nil || model == nil || model.NItems() == 0 {
			return
		}
		obj := model.Item(0)
		if obj == nil {
			return
		}
		file, ok := obj.Cast().(*gio.File)
		if !ok || file == nil {
			return
		}
		if path := file.Path(); path != "" {
			e.setOutputDir(path)
		}
	})
}

// parentWindow exposes the window as its base class, which is what the
// asynchronous dialogs accept. ApplicationWindow embeds Window by value.
func parentWindow(w *gtk.ApplicationWindow) *gtk.Window {
	if w == nil {
		return nil
	}
	return &w.Window
}

func (e *editor) setOutputDir(dir string) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		e.status.SetText("Cannot use that folder: " + err.Error())
		return
	}
	e.outputDir = dir
	e.outDir.SetText("Saves to: " + dir)
	e.outDir.SetTooltipText(dir)
	e.status.SetText("Renders will go to: " + dir)
}

func (e *editor) render() {
	text := e.text.Text()
	if strings.TrimSpace(text) == "" {
		e.status.SetText("Enter some text before rendering")
		return
	}
	duration := int(e.duration.Value())
	size := int(e.size.Value())
	speed := e.speed.Value()
	color := strings.TrimSpace(e.color.Text())
	if !strings.HasPrefix(color, "#") || len(color) != 7 {
		e.status.SetText("Color must look like #RRGGBB")
		return
	}

	path := filepath.Join(e.outputDir, outputName(time.Now()))
	font := e.selectedFont()
	color = "0x" + strings.TrimPrefix(color, "#")
	args := buildRenderArgs(font, text, color, size, duration, speed, path)
	e.status.SetText("Rendering with FFmpeg...")
	go func() {
		cmd := exec.Command("ffmpeg", args...)
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			glib.IdleAdd(func() { e.status.SetText("FFmpeg failed: " + err.Error()) })
			return
		}
		frames, err := buildPreviewFrames(path, e.cacheDir(), canvasWidth, canvasHeight, duration)
		if err != nil {
			glib.IdleAdd(func() { e.status.SetText("Done: " + path) })
			return
		}
		glib.IdleAdd(func() {
			e.showPreview(frames)
			e.status.SetText("Done: " + path)
		})
	}()
}

// cacheDir is a single stable folder for preview copies, so repeated renders
// do not leave a new temporary directory behind every time.
func (e *editor) cacheDir() string {
	if e.previewDir != "" {
		return e.previewDir
	}
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "muse")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		dir = os.TempDir()
	}
	e.previewDir = dir
	return dir
}

// previewFps is the frame rate the in-window preview plays back at. The
// exported video stays at 30 fps; 15 is plenty to judge the motion.
const previewFps = 15

// sourceFps is the frame rate every render is encoded at.
const sourceFps = 30

// buildPreviewFrames turns the finished render into a numbered JPEG sequence
// over a dark background.
//
// A GtkVideo widget is not usable here: it relies on GStreamer, and a system
// without a Matroska or WebM demuxer cannot open the file at all. Images go
// through GdkPixbuf instead, which every GTK install already has, so the
// preview needs no codec plugins whatsoever.
//
// The fps filter is deliberately avoided: on this graph it duplicates frames
// and stretches a 5 second clip into nearly three minutes.
func buildPreviewFrames(source, dir string, width, height, duration int) ([]string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	// Clear the sequences of earlier renders first, so only one survives.
	if entries, err := os.ReadDir(dir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() && strings.HasPrefix(entry.Name(), "preview-") {
				os.RemoveAll(filepath.Join(dir, entry.Name()))
			}
		}
	}

	seq := filepath.Join(dir, "preview-"+strconv.FormatInt(time.Now().UnixNano(), 36))
	if err := os.MkdirAll(seq, 0o755); err != nil {
		return nil, err
	}
	// The background needs an explicit duration, otherwise the endless colour
	// source keeps the overlay graph alive and ffmpeg never finishes. The
	// libvpx decoder is required because the native vp9 one drops the alpha
	// plane, which would leave the preview empty.
	args := []string{"-y",
		"-c:v", "libvpx-vp9", "-i", source,
		"-f", "lavfi", "-i", fmt.Sprintf("color=c=0x141414:s=%dx%d:r=%d:d=%d", width, height, sourceFps, duration),
		"-filter_complex", fmt.Sprintf("[1:v][0:v]overlay,scale=%d:-2", previewWidth),
		"-q:v", "6", "-start_number", "0", filepath.Join(seq, "f%05d.jpg"),
	}
	if err := exec.Command("ffmpeg", args...).Run(); err != nil {
		os.RemoveAll(seq)
		return nil, err
	}
	entries, err := os.ReadDir(seq)
	if err != nil {
		return nil, err
	}
	all := make([]string, 0, len(entries))
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".jpg") {
			all = append(all, filepath.Join(seq, entry.Name()))
		}
	}
	sort.Strings(all)
	if len(all) == 0 {
		os.RemoveAll(seq)
		return nil, fmt.Errorf("preview produced no frames")
	}

	// Thin the sequence down for playback instead of filtering it in ffmpeg.
	stride := sourceFps / previewFps
	if stride < 1 {
		stride = 1
	}
	frames := make([]string, 0, len(all)/stride+1)
	for i := 0; i < len(all); i += stride {
		frames = append(frames, all[i])
	}
	return frames, nil
}

// showPreview starts the frame animation, replacing any running one.
func (e *editor) showPreview(frames []string) {
	e.stopPreview()

	e.previewFrames = frames
	e.previewIndex = 0

	pixbuf, err := gdkpixbuf.NewPixbufFromFileAtScale(frames[0], previewWidth, -1, false)
	if err != nil {
		e.status.SetText("Preview failed: " + err.Error())
		return
	}
	e.preview.SetPixbuf(pixbuf)
	e.preview.SetVisible(true)
	e.previewHint.SetVisible(false)

	interval := uint(1000 / previewFps)
	e.previewTimer = glib.TimeoutAdd(interval, func() bool {
		if len(e.previewFrames) == 0 {
			return false
		}
		e.previewIndex = (e.previewIndex + 1) % len(e.previewFrames)
		pb, err := gdkpixbuf.NewPixbufFromFileAtScale(e.previewFrames[e.previewIndex], previewWidth, -1, false)
		if err != nil {
			return true
		}
		e.preview.SetPixbuf(pb)
		return true
	})
}

func (e *editor) stopPreview() {
	if e.previewTimer != 0 {
		glib.SourceRemove(e.previewTimer)
		e.previewTimer = 0
	}
}

// buildRenderArgs assembles the ffmpeg command for one render.
func buildRenderArgs(font, text, color string, size, duration int, speed float64, path string) []string {
	filter := blurWakeFilter(font, text, color, size, canvasWidth, canvasHeight, duration, speed)
	// The canvas has to be built as yuva420p, otherwise the colour source
	// negotiates yuv420p with no alpha plane and drawtext cannot write alpha.
	canvas := fmt.Sprintf("color=c=black@0.0:s=%dx%d:r=30:d=%d,format=yuva420p", canvasWidth, canvasHeight, duration)
	return []string{"-y", "-f", "lavfi", "-i", canvas,
		"-filter_complex", filter, "-map", "[out]",
		"-c:v", "libvpx-vp9", "-pix_fmt", "yuva420p", "-auto-alt-ref", "0", path}
}

// selectedFont returns the font family chosen in the dropdown, falling back to
// the default when the index is out of range.
func (e *editor) selectedFont() string {
	if len(e.fonts) == 0 {
		return defaultFont
	}
	i := int(e.font.Selected())
	if i < 0 || i >= len(e.fonts) {
		return defaultFont
	}
	return e.fonts[i]
}

// outputName builds a timestamped file name: muse-text-HH-MM-SS-DD-MM-YY.webm
func outputName(t time.Time) string {
	return "muse-text-" + t.Format("15-04-05-02-01-06") + ".webm"
}

// layoutLines wraps text to the canvas width and centres the resulting block.
func layoutLines(font, text string, size, width, height int) []string {
	lines := wrapText(font, size, width-2*textMargin-wrapSlack, text)
	if len(lines) == 0 {
		return []string{""}
	}
	// Shrink to fit vertically when there are many lines.
	for len(lines) > 1 && lineHeightFor(font, size)*len(lines) > height-2*textMargin {
		size = size * 9 / 10
		if size < 8 {
			break
		}
		lines = wrapText(font, size, width-2*textMargin-wrapSlack, text)
	}
	return lines
}

// lineHeightFor approximates the line spacing for the given font size.
func lineHeightFor(font string, size int) int {
	h := measureText(font, size, "Ag")
	if h <= 0 {
		return size + size/4
	}
	// A single line renders at the font's ascent+descent; add a little leading.
	return h + size/6
}

// blurWakeFilter implements the full motion curve:
// y: -100 -> 0 -> +100, alpha: 0 -> 1 -> 0, blur: 20 -> 0 -> 20.
// The first and last parts use cubic ease-out and ease-in respectively.
// Long text is wrapped so it never leaves the canvas.
func blurWakeFilter(font, text, color string, size, width, height, duration int, speed float64) string {
	in := speed
	if in > float64(duration)/2 {
		in = float64(duration) / 2
	}
	if in < 0.1 {
		in = 0.1
	}
	outStart := float64(duration) - in
	alpha := fmt.Sprintf("if(lt(t\\,%0.3f)\\,1-pow(1-t/%0.3f\\,3)\\,if(lt(t\\,%0.3f)\\,1\\,pow(1-(t-%0.3f)/%0.3f\\,3)))", in, in, outStart, outStart, in)
	offset := fmt.Sprintf("if(lt(t\\,%0.3f)\\,-100+100*(1-pow(1-t/%0.3f\\,3))\\,if(lt(t\\,%0.3f)\\,0\\,100*pow((t-%0.3f)/%0.3f\\,3)))", in, in, outStart, outStart, in)

	lines := layoutLines(font, text, size, width, height)
	lineHeight := lineHeightFor(font, size)
	blockHeight := lineHeight * len(lines)
	top := (height - blockHeight) / 2

	draws := make([]string, 0, len(lines))
	for i, line := range lines {
		y := fmt.Sprintf("%d+%s", top+i*lineHeight, offset)
		draws = append(draws, fmt.Sprintf("drawtext=font='%s':text='%s':fontcolor=%s:fontsize=%d:x=(w-text_w)/2:y='%s':alpha='%s'", escapeFilter(font), escapeFilter(line), color, size, y, alpha))
	}
	textLayer := strings.Join(draws, ",")

	// Blur is 20 px on the first frame, drops to 0 in the middle and returns to
	// 20 px at the end. boxblur only accepts a fixed radius, so the alpha plane
	// is produced twice, blurred and sharp, then cross faded by time. The
	// colour plane keeps the drawn text, so a viewer that ignores alpha still
	// shows readable letters on black instead of a flat block of colour.
	factor := fmt.Sprintf("if(lt(T,%0.3f),pow(1-T/%0.3f,3),if(lt(T,%0.3f),0,1-pow(1-(T-%0.3f)/%0.3f,3)))", in, in, outStart, outStart, in)
	return textLayer + ",split[ta][tb]" +
		";[ta]split[tc][td]" +
		";[td]alphaextract,format=gray[aa]" +
		";[tb]boxblur=luma_radius=20:luma_power=1:alpha_radius=20:alpha_power=1[tb2]" +
		";[tb2]alphaextract,format=gray[ab]" +
		";[aa][ab]blend=all_expr='A*(1-" + factor + ")+B*" + factor + "'[amix]" +
		";[tc][amix]alphamerge,format=yuva420p[out]"
}

func escapeFilter(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "'", "\\'")
	s = strings.ReplaceAll(s, ":", "\\:")
	return s
}
