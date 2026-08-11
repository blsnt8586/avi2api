package videospec

type Spec struct {
	DefaultDuration             int
	Durations                   []int
	DefaultSize                 string
	Sizes                       []string
	DefaultResolution           string
	Resolutions                 []string
	MaxReferenceImages          int
	MaxReferenceImagesWithVideo int
	SupportsStartEndFrame       bool
	SupportsEndFrame            bool
	RequiresStartFrame          bool
	MaxReferenceVideos          int
	MaxReferenceAudios          int
	MinVideoDuration            float64
	MaxVideoDuration            float64
	MinReferenceVideoDimension  int
	MaxReferenceVideoDimension  int
	VideoReferenceResolutions   []string
	OmitDurationWithVideo       bool
	MaxReferenceVideoBytes      int64
	MaxAudioDuration            float64
	SupportsGenerateAudio       bool
	AlwaysGenerateAudio         bool
	UsesResolutionMode          bool
	UsesExactDimensions         bool
	ResolutionBySize            map[string]string
}

var standardSizes = []string{"1280x720", "720x1280"}

var specs = map[string]Spec{
	"seedance-2.0": {
		DefaultDuration: 8, Durations: integerRange(4, 15), DefaultSize: "1280x720", Sizes: standardSizes,
		DefaultResolution: "720p", Resolutions: []string{"480p", "720p", "1080p", "2160p"},
		MaxReferenceImages: 4, SupportsStartEndFrame: true, SupportsEndFrame: true, MaxReferenceVideos: 3, MaxReferenceAudios: 1, MaxVideoDuration: 15, MaxAudioDuration: 15,
		SupportsGenerateAudio: true, UsesResolutionMode: true,
	},
	"seedance-2.0-fast": {
		DefaultDuration: 8, Durations: integerRange(4, 15), DefaultSize: "1280x720", Sizes: standardSizes,
		DefaultResolution: "720p", Resolutions: []string{"480p", "720p"},
		MaxReferenceImages: 4, SupportsStartEndFrame: true, SupportsEndFrame: true, MaxReferenceVideos: 3, MaxReferenceAudios: 1, MaxVideoDuration: 15, MaxAudioDuration: 15,
		SupportsGenerateAudio: true, UsesResolutionMode: true,
	},
	"seedance-2.0-mini": {
		DefaultDuration: 8, Durations: integerRange(4, 15), DefaultSize: "1280x720", Sizes: standardSizes,
		DefaultResolution: "720p", Resolutions: []string{"480p", "720p"},
		MaxReferenceImages: 4, SupportsStartEndFrame: true, SupportsEndFrame: true, MaxReferenceVideos: 3, MaxReferenceAudios: 1, MaxVideoDuration: 15, MaxAudioDuration: 15,
		SupportsGenerateAudio: true,
	},
	"flux-3-video": {
		DefaultDuration: 8, Durations: integerRange(5, 20), DefaultSize: "1280x720",
		Sizes: []string{
			"1470x630", "1360x680", "1280x720", "1112x834", "960x960", "834x1112", "720x1280",
			"2520x1080", "2160x1080", "1920x1080", "1440x1080", "1440x1440", "1080x1440", "1080x1920",
		},
		DefaultResolution: "720p", Resolutions: []string{"720p", "1080p"},
		SupportsStartEndFrame: true, SupportsEndFrame: true,
		MaxReferenceVideos: 1, MaxVideoDuration: 15.05, MaxReferenceVideoBytes: 50_000_000,
		SupportsGenerateAudio: true, UsesExactDimensions: true,
		ResolutionBySize: map[string]string{
			"1470x630": "720p", "1360x680": "720p", "1280x720": "720p", "1112x834": "720p", "960x960": "720p", "834x1112": "720p", "720x1280": "720p",
			"2520x1080": "1080p", "2160x1080": "1080p", "1920x1080": "1080p", "1440x1080": "1080p", "1440x1440": "1080p", "1080x1440": "1080p", "1080x1920": "1080p",
		},
	},
	"veo-3.1": {
		DefaultDuration: 8, Durations: []int{4, 6, 8}, DefaultSize: "1280x720", Sizes: standardSizes,
		DefaultResolution: "720p", Resolutions: []string{"720p", "1080p", "2160p"},
		MaxReferenceImages: 3, SupportsStartEndFrame: true, SupportsEndFrame: true, SupportsGenerateAudio: true,
		UsesResolutionMode: true,
	},
	"veo-3.1-fast": {
		DefaultDuration: 8, Durations: []int{4, 6, 8}, DefaultSize: "1280x720", Sizes: standardSizes,
		DefaultResolution: "720p", Resolutions: []string{"720p", "1080p", "2160p"},
		SupportsStartEndFrame: true, SupportsEndFrame: true, SupportsGenerateAudio: true, UsesResolutionMode: true,
	},
	"kling-o3-omni": {
		DefaultDuration: 5, Durations: integerRange(3, 15), DefaultSize: "1920x1080",
		Sizes: []string{
			"1280x720", "720x1280", "960x960",
			"1920x1080", "1080x1920", "1440x1440",
			"3840x2160", "2160x3840", "2880x2880",
		},
		DefaultResolution: "1080p", Resolutions: []string{"720p", "1080p", "2160p"},
		MaxReferenceImages: 7, MaxReferenceImagesWithVideo: 4,
		SupportsStartEndFrame: true, SupportsEndFrame: true,
		MaxReferenceVideos: 1, MinVideoDuration: 3, MaxVideoDuration: 10.05,
		MinReferenceVideoDimension: 720, MaxReferenceVideoDimension: 2160,
		VideoReferenceResolutions: []string{"720p", "1080p"}, OmitDurationWithVideo: true,
		SupportsGenerateAudio: true, UsesExactDimensions: true,
		ResolutionBySize: map[string]string{
			"1280x720": "720p", "720x1280": "720p", "960x960": "720p",
			"1920x1080": "1080p", "1080x1920": "1080p", "1440x1440": "1080p",
			"3840x2160": "2160p", "2160x3840": "2160p", "2880x2880": "2160p",
		},
	},
	"minimax-h3": {
		DefaultDuration: 5, Durations: integerRange(5, 15), DefaultSize: "2560x1440",
		Sizes:             []string{"3360x1440", "2560x1440", "1920x1440", "1440x1440", "1440x1920", "1440x2560"},
		DefaultResolution: "1440p", Resolutions: []string{"1440p"}, MaxReferenceImages: 5,
		SupportsStartEndFrame: true, SupportsEndFrame: true, MaxReferenceAudios: 3, MaxAudioDuration: 15,
		SupportsGenerateAudio: true, AlwaysGenerateAudio: true, UsesExactDimensions: true,
	},
	"grok-imagine-1.5": {
		DefaultDuration: 6, Durations: integerRange(3, 15), DefaultSize: "736x400",
		Sizes: []string{
			"736x400", "400x736", "544x544",
			"1280x720", "720x1280", "960x960",
			"1888x1072", "1072x1888", "1424x1424",
		},
		DefaultResolution: "480p", Resolutions: []string{"480p", "720p", "1080p"},
		SupportsStartEndFrame: true, RequiresStartFrame: true,
		SupportsGenerateAudio: true, UsesExactDimensions: true,
		ResolutionBySize: map[string]string{
			"736x400": "480p", "400x736": "480p", "544x544": "480p",
			"1280x720": "720p", "720x1280": "720p", "960x960": "720p",
			"1888x1072": "1080p", "1072x1888": "1080p", "1424x1424": "1080p",
		},
	},
}

func Get(model string) (Spec, bool) {
	spec, ok := specs[model]
	return spec, ok
}

func (s Spec) SupportsDuration(duration int) bool {
	return contains(s.Durations, duration)
}

func (s Spec) SupportsResolution(resolution string) bool {
	return contains(s.Resolutions, resolution)
}

func (s Spec) SupportsReferenceVideoDimensions(width, height int) bool {
	if width <= 0 || height <= 0 {
		return false
	}
	if s.MinReferenceVideoDimension > 0 && (width < s.MinReferenceVideoDimension || height < s.MinReferenceVideoDimension) {
		return false
	}
	if s.MaxReferenceVideoDimension > 0 && (width > s.MaxReferenceVideoDimension || height > s.MaxReferenceVideoDimension) {
		return false
	}
	return true
}

func (s Spec) SupportsSize(size string) bool {
	return contains(s.Sizes, size)
}

func (s Spec) SupportsVideoReferenceResolution(resolution string) bool {
	return len(s.VideoReferenceResolutions) == 0 || contains(s.VideoReferenceResolutions, resolution)
}

func (s Spec) ResolutionForSize(size string) (string, bool) {
	resolution, ok := s.ResolutionBySize[size]
	return resolution, ok
}

func (s Spec) DefaultSizeForResolution(resolution string) (string, bool) {
	if resolution == "" || len(s.ResolutionBySize) == 0 {
		return "", false
	}
	for _, size := range s.Sizes {
		if s.ResolutionBySize[size] == resolution {
			return size, true
		}
	}
	return "", false
}

func integerRange(minimum, maximum int) []int {
	values := make([]int, 0, maximum-minimum+1)
	for value := minimum; value <= maximum; value++ {
		values = append(values, value)
	}
	return values
}

func contains[T comparable](values []T, target T) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
