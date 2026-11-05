package graphapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LukasHirt/groupkit/store"
	"github.com/LukasHirt/groupkit/vcard"
)

func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 200, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func testJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h)), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func testGIF(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := gif.Encode(&b, image.NewPaletted(image.Rect(0, 0, 2, 2), color.Palette{color.Black, color.White}), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func putPhoto(f *fixture, target, token, contentType string, data []byte, hdr ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPut, target, bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec
}

func TestPhotoInCards(t *testing.T) {
	jpg := testJPEG(t, 3, 2)
	b64 := base64.StdEncoding.EncodeToString(jpg)
	for name, card := range map[string]string{
		"v3 typed":   "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:A\r\nPHOTO;ENCODING=b;TYPE=JPEG:" + b64 + "\r\nEND:VCARD\r\n",
		"v3 untyped": "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:A\r\nPHOTO;ENCODING=B:" + b64 + "\r\nEND:VCARD\r\n",
		"v4 data":    "BEGIN:VCARD\r\nVERSION:4.0\r\nFN:A\r\nPHOTO:data:image/jpeg;base64," + b64 + "\r\nEND:VCARD\r\n",
	} {
		c, err := vcard.Parse([]byte(card))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		data, mime, ok := cardPhoto(c)
		if !ok || mime != "image/jpeg" || !bytes.Equal(data, jpg) {
			t.Errorf("%s: %v %q %d bytes", name, ok, mime, len(data))
		}
	}
	for name, card := range map[string]string{
		"none":      "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:A\r\nEND:VCARD\r\n",
		"url":       "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:A\r\nPHOTO;VALUE=uri:https://example.org/a.jpg\r\nEND:VCARD\r\n",
		"v4 url":    "BEGIN:VCARD\r\nVERSION:4.0\r\nFN:A\r\nPHOTO:https://example.org/a.jpg\r\nEND:VCARD\r\n",
		"not b64":   "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:A\r\nPHOTO;ENCODING=b;TYPE=JPEG:!!!\r\nEND:VCARD\r\n",
		"gif":       "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:A\r\nPHOTO;ENCODING=b;TYPE=GIF:" + base64.StdEncoding.EncodeToString(testGIF(t)) + "\r\nEND:VCARD\r\n",
		"junk data": "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:A\r\nPHOTO;ENCODING=b:" + base64.StdEncoding.EncodeToString([]byte("hello")) + "\r\nEND:VCARD\r\n",
	} {
		c, err := vcard.Parse([]byte(card))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, _, ok := cardPhoto(c); ok {
			t.Errorf("%s: found a photo", name)
		}
	}
}

func TestSetCardPhotoByVersion(t *testing.T) {
	png1 := testPNG(t, 2, 2)
	v3, _ := vcard.Parse([]byte("BEGIN:VCARD\r\nVERSION:3.0\r\nFN:A\r\nPHOTO;ENCODING=b;TYPE=JPEG:AAAA\r\nPHOTO;VALUE=uri:https://example.org/old.jpg\r\nX-KEEP:1\r\nEND:VCARD\r\n"))
	setCardPhoto(v3, png1, "image/png", fixedNow)
	lines := cardLines(t, v3)
	if !contains(lines, "PHOTO;ENCODING=b;TYPE=PNG:"+base64.StdEncoding.EncodeToString(png1)) || !contains(lines, "X-KEEP:1") || !contains(lines, "REV:20270203T040506Z") {
		t.Errorf("v3: %v", lines)
	}
	n := 0
	for _, l := range lines {
		if strings.HasPrefix(l, "PHOTO") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d PHOTO lines, want the old ones replaced", n)
	}
	v4, _ := vcard.Parse([]byte("BEGIN:VCARD\r\nVERSION:4.0\r\nFN:A\r\nEND:VCARD\r\n"))
	setCardPhoto(v4, png1, "image/png", fixedNow)
	if !contains(cardLines(t, v4), "PHOTO:data:image/png;base64,"+base64.StdEncoding.EncodeToString(png1)) {
		t.Errorf("v4: %v", cardLines(t, v4))
	}
	if !removeCardPhoto(v4, fixedNow) || removeCardPhoto(v4, fixedNow) {
		t.Error("removeCardPhoto should report a change once")
	}
}

func TestContactPhotoRoutes(t *testing.T) {
	f := newFixture(t)
	ref, collID := einsteinBook(t, f)
	putCard(t, f, ref, "ada.vcf", "BEGIN:VCARD\nVERSION:3.0\nUID:ada\nFN:Ada\nX-CUSTOM:keep\nEND:VCARD\n")
	id := EventID(collID, "ada.vcf")
	base := "/graph/v1.0/me/contacts/" + id + "/photo"

	if rec := f.do(http.MethodGet, base, "einstein", ""); rec.Code != http.StatusNotFound {
		t.Errorf("no photo yet: %d", rec.Code)
	}
	if rec := f.do(http.MethodGet, base+"/$value", "einstein", ""); rec.Code != http.StatusNotFound {
		t.Errorf("no photo yet ($value): %d", rec.Code)
	}
	if got := decodeBody[map[string]any](t, f.do(http.MethodGet, "/graph/v1.0/me/contacts/"+id, "einstein", "")); got["groupware.hasPhoto"] != false {
		t.Errorf("hasPhoto = %v", got["groupware.hasPhoto"])
	}

	img := testPNG(t, 4, 3)
	rec := putPhoto(f, base+"/$value", "einstein", "image/png", img)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body.String())
	}
	rec = f.do(http.MethodGet, base+"/$value", "einstein", "")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" || !bytes.Equal(rec.Body.Bytes(), img) {
		t.Errorf("GET $value: %d %q %d bytes", rec.Code, rec.Header().Get("Content-Type"), rec.Body.Len())
	}
	rec = f.do(http.MethodGet, base, "einstein", "")
	meta := decodeBody[map[string]any](t, rec)
	if rec.Code != http.StatusOK || meta["@odata.mediaContentType"] != "image/png" || meta["width"] != float64(4) || meta["height"] != float64(3) {
		t.Errorf("GET metadata: %d %v", rec.Code, meta)
	}
	if got := decodeBody[map[string]any](t, f.do(http.MethodGet, "/graph/v1.0/me/contacts/"+id, "einstein", "")); got["groupware.hasPhoto"] != true {
		t.Errorf("hasPhoto = %v", got["groupware.hasPhoto"])
	}
	// The card for DAV clients has the photo and everything else it had.
	obj, _ := f.st.GetObject(context.Background(), ref, "ada.vcf")
	if !strings.Contains(string(obj.Data), "PHOTO;ENCODING=b;TYPE=PNG:") || !strings.Contains(string(obj.Data), "X-CUSTOM:keep") {
		t.Errorf("stored card:\n%s", obj.Data)
	}
	// Below a folder.
	if rec := f.do(http.MethodGet, "/graph/v1.0/me/contactFolders/"+CalendarID(collID)+"/contacts/"+id+"/photo/$value", "einstein", ""); rec.Code != http.StatusOK {
		t.Errorf("GET below the folder: %d", rec.Code)
	}

	// A JPEG replaces the PNG, and the sniffed type counts, not the header.
	jpg := testJPEG(t, 5, 5)
	if rec := putPhoto(f, base+"/$value", "einstein", "application/octet-stream", jpg); rec.Code != http.StatusOK {
		t.Fatalf("PUT jpeg: %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.do(http.MethodGet, base+"/$value", "einstein", ""); rec.Header().Get("Content-Type") != "image/jpeg" || !bytes.Equal(rec.Body.Bytes(), jpg) {
		t.Errorf("after replace: %q", rec.Header().Get("Content-Type"))
	}

	// Refusals leave the photo as it is.
	big := append(append([]byte{}, jpg...), make([]byte, maxPhotoBytes)...)
	for _, tc := range []struct {
		name string
		body []byte
		want int
	}{
		{"gif", testGIF(t), http.StatusUnsupportedMediaType},
		{"text", []byte("not an image at all"), http.StatusUnsupportedMediaType},
		{"too large", big, http.StatusRequestEntityTooLarge},
		{"empty", nil, http.StatusBadRequest},
		{"corrupt", append([]byte{0xff, 0xd8, 0xff, 0xe0}, "garbage"...), http.StatusBadRequest},
	} {
		rec := putPhoto(f, base+"/$value", "einstein", "image/jpeg", tc.body)
		if rec.Code != tc.want {
			t.Errorf("%s: %d, want %d: %.100s", tc.name, rec.Code, tc.want, rec.Body.String())
			continue
		}
		if decodeError(t, rec).Error.Code == "" {
			t.Errorf("%s: not a Graph error", tc.name)
		}
	}
	if rec := f.do(http.MethodGet, base+"/$value", "einstein", ""); !bytes.Equal(rec.Body.Bytes(), jpg) {
		t.Error("a refused upload changed the photo")
	}

	// Exactly the limit is fine.
	atLimit := append(append([]byte{}, jpg...), make([]byte, maxPhotoBytes-len(jpg))...)
	if rec := putPhoto(f, base+"/$value", "einstein", "image/jpeg", atLimit); rec.Code != http.StatusOK {
		t.Errorf("a photo of %d bytes: %d %.100s", len(atLimit), rec.Code, rec.Body.String())
	}

	if rec := f.do(http.MethodDelete, base, "einstein", ""); rec.Code != http.StatusNoContent {
		t.Errorf("DELETE: %d", rec.Code)
	}
	if rec := f.do(http.MethodGet, base+"/$value", "einstein", ""); rec.Code != http.StatusNotFound {
		t.Errorf("after DELETE: %d", rec.Code)
	}
	if rec := f.do(http.MethodDelete, base, "einstein", ""); rec.Code != http.StatusNotFound {
		t.Errorf("second DELETE: %d", rec.Code)
	}
}

func TestContactPhotoIfMatchAndErrors(t *testing.T) {
	f := newFixture(t)
	ref, collID := einsteinBook(t, f)
	putCard(t, f, ref, "ada.vcf", "BEGIN:VCARD\nVERSION:3.0\nUID:ada\nFN:Ada\nEND:VCARD\n")
	putCard(t, f, ref, "club.vcf", "BEGIN:VCARD\nVERSION:3.0\nUID:club\nX-ADDRESSBOOKSERVER-KIND:group\nFN:Club\nEND:VCARD\n")
	id := EventID(collID, "ada.vcf")
	target := "/graph/v1.0/me/contacts/" + id + "/photo/$value"
	img := testPNG(t, 2, 2)
	if rec := putPhoto(f, target, "einstein", "image/png", img, "If-Match", `W/"stale"`); rec.Code != http.StatusPreconditionFailed {
		t.Errorf("stale PUT: %d", rec.Code)
	}
	obj, _ := f.st.GetObject(context.Background(), ref, "ada.vcf")
	if rec := putPhoto(f, target, "einstein", "image/png", img, "If-Match", etagHeader(obj.ETag)); rec.Code != http.StatusOK {
		t.Errorf("fresh PUT: %d", rec.Code)
	}
	for _, tc := range []struct {
		method, target string
		want           int
	}{
		{http.MethodGet, "/graph/v1.0/me/contacts/" + EventID(collID, "club.vcf") + "/photo/$value", 404},
		{http.MethodPut, "/graph/v1.0/me/contacts/" + EventID(collID, "club.vcf") + "/photo/$value", 404},
		{http.MethodGet, "/graph/v1.0/me/contacts/" + EventID(collID, "nope.vcf") + "/photo", 404},
		{http.MethodGet, "/graph/v1.0/me/contacts/bad/photo", 400},
		{http.MethodPut, "/graph/v1.0/me/contacts/" + id + "/photo", 405},
		{http.MethodPost, target, 405},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(tc.method, tc.target, bytes.NewReader(img))
		req.Header.Set("Authorization", "Bearer einstein")
		req.Header.Set("Content-Type", "image/png")
		f.handler.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("%s %s = %d, want %d", tc.method, tc.target, rec.Code, tc.want)
		}
	}
}

func TestContactPhotoReadOnlyShare(t *testing.T) {
	f, col := sharedBookFixture(t, store.AccessRead)
	img := testPNG(t, 2, 2)
	card := "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:pierre\r\nFN:Pierre\r\nPHOTO;ENCODING=b;TYPE=PNG:" + base64.StdEncoding.EncodeToString(img) + "\r\nEND:VCARD\r\n"
	if _, err := f.st.PutObject(context.Background(), col.Ref, "pierre.vcf", []byte(card), store.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	target := "/graph/v1.0/me/contactFolders/" + CalendarID(col.ID) + "/contacts/" + EventID(col.ID, "pierre.vcf") + "/photo/$value"
	if rec := f.do(http.MethodGet, target, "einstein", ""); rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), img) {
		t.Errorf("a recipient reads the photo: %d", rec.Code)
	}
	if rec := putPhoto(f, target, "einstein", "image/png", img); rec.Code != http.StatusForbidden {
		t.Errorf("PUT: %d, want 403", rec.Code)
	}
	if rec := f.do(http.MethodDelete, target, "einstein", ""); rec.Code != http.StatusForbidden {
		t.Errorf("DELETE: %d, want 403", rec.Code)
	}
}
