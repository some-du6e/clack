package popups

import "github.com/some-du6e/clack/internal/richtext"

// GalleryItem is one file of a media viewer gallery.
type GalleryItem struct {
	Img  richtext.Image
	File *richtext.File
}

// NewFileGallery is the media viewer opened with fileChannelMediaInfo
// (handleFileContainerClick from the Files & links tab, CDN build
// 0e695dd18302: MediaViewer viWS builds its queue from the info's files,
// the ones fileIsViewableInMediaViewer keeps, and opens at the clicked
// file): ← / → and the arrow buttons step through the channel's loaded
// media instead of one message's images. Clack's viewer shows images, so
// the caller passes those (its videos still open their page).
func NewFileGallery(h Host, items []GalleryItem, idx int) *Lightbox {
	lb := &Lightbox{h: h, zoom: 1}
	for _, it := range items {
		if it.Img.URL == "" {
			continue
		}
		lb.items = append(lb.items, lbItem{img: it.Img, file: it.File})
	}
	if len(lb.items) == 0 {
		return nil
	}
	lb.idx = min(max(idx, 0), len(lb.items)-1)
	return lb
}
