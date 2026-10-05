// Package guidance holds the fixed catalogue of emotion handling guides shown to parents
package guidance

import (
	"strings"

	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
)

const defaultBannerBase = "https://storage.kibooz.id"

type Guidance struct {
	ID         string
	Title      string
	Category   string
	BannerPath string
}

var catalogue = map[constants.Mood]Guidance{
	constants.MoodSenang: {
		ID:         "guidance-senang-4step",
		Title:      "Rayakan Hari Ceria Bersama Anak",
		Category:   "Senang / Ceria",
		BannerPath: "guidance/banner_ceria.png",
	},
	constants.MoodSedih: {
		ID:         "guidance-sedih-4step",
		Title:      "Temani Anak Saat Merasa Sedih",
		Category:   "Sedih",
		BannerPath: "guidance/banner_temani.png",
	},
	constants.MoodMarah: {
		ID:         "guidance-marah-4step",
		Title:      "Bantu Anak Menenangkan Diri",
		Category:   "Marah",
		BannerPath: "guidance/banner_tenang.png",
	},
	constants.MoodBingung: {
		ID:         "guidance-bingung-4step",
		Title:      "Berikan Rasa Aman Pada Anak",
		Category:   "Bingung / Ragu",
		BannerPath: "guidance/banner_aman.png",
	},
}

// ForMood returns the guide that matches the mood
func ForMood(mood constants.Mood) (Guidance, bool) {
	item, ok := catalogue[mood]

	return item, ok
}

// Exists reports whether id belongs to a known guide
func Exists(id string) bool {
	for _, item := range catalogue {
		if item.ID == id {
			return true
		}
	}

	return false
}

// BannerURL builds the public URL of the guide banner
func (g Guidance) BannerURL(publicBase string) string {
	base := strings.TrimRight(publicBase, "/")
	if base == "" {
		base = defaultBannerBase
	}

	return base + "/" + g.BannerPath
}
