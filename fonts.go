package main

/*
#cgo pkg-config: pangocairo
#include <pango/pangocairo.h>
#include <pango/pango-utils.h>
#include <string.h>
#include <stdlib.h>

// Pango 1.56 changed pango_font_map_list_families to hand the array back
// through a triple pointer. Both shapes are handled so Muse also builds against
// the older Pango shipped by distributions such as Debian 12.

// muse_font_count returns the number of font families known to Pango.
static int muse_font_count(void) {
	PangoFontMap *map = pango_cairo_font_map_get_default();
	int n = 0;
	PangoFontFamily **families = NULL;
#if PANGO_VERSION_CHECK(1, 56, 0)
	pango_font_map_list_families(map, &families, &n);
#else
	pango_font_map_list_families(map, families, &n);
#endif
	g_free(families);
	return n;
}

// muse_font_name copies the family name at index, or NULL when out of range.
static char *muse_font_name(int index) {
	PangoFontMap *map = pango_cairo_font_map_get_default();
	int n = 0;
	PangoFontFamily **families = NULL;
#if PANGO_VERSION_CHECK(1, 56, 0)
	pango_font_map_list_families(map, &families, &n);
#else
	pango_font_map_list_families(map, families, &n);
#endif
	if (index < 0 || index >= n) {
		g_free(families);
		return NULL;
	}
	char *name = g_strdup(pango_font_family_get_name(families[index]));
	g_free(families);
	return name;
}

static void muse_string_free(char *s) {
	g_free(s);
}

// MuseText keeps the layout together with the context and the font
// description it depends on. A layout keeps using both of them, so they must
// outlive it and may only be released after it.
typedef struct {
	PangoLayout *layout;
	PangoContext *ctx;
	PangoFontDescription *desc;
} MuseText;

// muse_text_new builds a layout for the given family. size_px is a pixel value
// so it matches drawtext's fontsize, and max_width of 0 disables wrapping.
static MuseText *muse_text_new(const char *font, int size_px, int max_width, const char *text) {
	MuseText *t = g_new0(MuseText, 1);
	PangoFontMap *map = pango_cairo_font_map_get_default();
	t->ctx = pango_font_map_create_context(map);

	t->desc = pango_font_description_from_string(font);
	if (t->desc == NULL) {
		t->desc = pango_font_description_from_string("Sans");
	}
	// Absolute size is expressed in device units, so pixels need PANGO_SCALE.
	pango_font_description_set_absolute_size(t->desc, size_px * PANGO_SCALE);

	t->layout = pango_layout_new(t->ctx);
	pango_layout_set_font_description(t->layout, t->desc);
	if (max_width > 0) {
		pango_layout_set_width(t->layout, max_width * PANGO_SCALE);
		pango_layout_set_wrap(t->layout, PANGO_WRAP_WORD_CHAR);
	}
	pango_layout_set_text(t->layout, text, -1);
	return t;
}

// muse_text_free releases the layout first, then what it was built from.
static void muse_text_free(MuseText *t) {
	if (t == NULL) {
		return;
	}
	// pango_layout_set_font_description takes over the description it is
	// given, so the layout frees it. The layout does not own the context,
	// which therefore has to outlive it.
	g_object_unref(t->layout);
	g_object_unref(t->ctx);
	g_free(t);
}

static void muse_text_pixel_size(MuseText *t, int *width, int *height) {
	pango_layout_get_pixel_size(t->layout, width, height);
}

// muse_wrap splits text into lines that fit the layout width and returns a NULL
// terminated array of newly allocated strings.
static char **muse_wrap(MuseText *t, const char *text, int *out_count) {
	*out_count = 0;
	if (t == NULL || text == NULL) {
		return NULL;
	}

	char **result = NULL;
	int n = 0;

	for (int i = 0;; i++) {
		PangoLayoutLine *line = pango_layout_get_line_readonly(t->layout, i);
		if (line == NULL) {
			break;
		}
		if (line->length == 0) {
			continue;
		}
		// start_index and length are byte offsets into the text we passed in.
		char *chunk = g_strndup(text + line->start_index, line->length);
		char *start = chunk;
		while (*start == ' ' || *start == '\t' || *start == '\n' || *start == '\r') {
			start++;
		}
		char *end = start + strlen(start);
		while (end > start && (end[-1] == ' ' || end[-1] == '\t' ||
		                       end[-1] == '\n' || end[-1] == '\r')) {
			end--;
		}
		*end = '\0';
		if (*start != '\0') {
			result = g_realloc(result, sizeof(char *) * (n + 1));
			result[n++] = g_strdup(start);
		}
		g_free(chunk);
	}

	result = g_realloc(result, sizeof(char *) * (n + 1));
	result[n] = NULL;
	*out_count = n;
	return result;
}

static void muse_array_free(char **arr, int count) {
	if (arr == NULL) {
		return;
	}
	for (int i = 0; i < count; i++) {
		g_free(arr[i]);
	}
	g_free(arr);
}
*/
import "C"

import (
	"sort"
	"strings"
	"unsafe"
)

// listFonts returns every font family available to Pango, sorted and without
// duplicates. Pango goes through fontconfig on Linux and CoreText on macOS, so
// the list matches what the system can actually render.
func listFonts() []string {
	count := int(C.muse_font_count())
	if count <= 0 {
		return nil
	}
	seen := make(map[string]struct{}, count)
	fonts := make([]string, 0, count)
	for i := 0; i < count; i++ {
		cName := C.muse_font_name(C.int(i))
		if cName == nil {
			continue
		}
		name := C.GoString(cName)
		C.muse_string_free(cName)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		fonts = append(fonts, name)
	}
	sort.Strings(fonts)
	return fonts
}

// measureText returns the width of text in pixels for the given family and
// pixel size, matching what drawtext produces.
func measureText(font string, sizePx int, text string) int {
	if text == "" {
		return 0
	}
	cFont := C.CString(font)
	defer C.free(unsafe.Pointer(cFont))
	cText := C.CString(text)
	defer C.free(unsafe.Pointer(cText))

	t := C.muse_text_new(cFont, C.int(sizePx), 0, cText)
	if t == nil {
		return 0
	}
	defer C.muse_text_free(t)

	var w, h C.int
	C.muse_text_pixel_size(t, &w, &h)
	return int(w)
}

// wrapText breaks text into lines that fit maxWidth pixels. Explicit newlines
// are always honoured and the result never contains empty lines.
func wrapText(font string, sizePx, maxWidth int, text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	if maxWidth <= 0 {
		maxWidth = 1
	}
	cFont := C.CString(font)
	defer C.free(unsafe.Pointer(cFont))
	cText := C.CString(text)
	defer C.free(unsafe.Pointer(cText))

	t := C.muse_text_new(cFont, C.int(sizePx), C.int(maxWidth), cText)
	if t == nil {
		return splitLines(text)
	}
	defer C.muse_text_free(t)

	var count C.int
	cArr := C.muse_wrap(t, cText, &count)
	if cArr == nil {
		return splitLines(text)
	}
	lines := make([]string, 0, int(count))
	for _, item := range unsafe.Slice(cArr, int(count)) {
		if item == nil {
			continue
		}
		lines = append(lines, C.GoString(item))
	}
	C.muse_array_free(cArr, count)
	if len(lines) == 0 {
		return splitLines(text)
	}
	return lines
}

func splitLines(text string) []string {
	raw := strings.Split(text, "\n")
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
