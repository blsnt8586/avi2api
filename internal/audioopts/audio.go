package audioopts

import "strings"

var voiceIDs = map[string]string{
	"roger": "CwhRBWXzGAHq8TQ4Fs17", "sarah": "EXAVITQu4vr4xnSDxMaL", "laura": "FGY2WhTYpPnrIDTdsKH5",
	"charlie": "IKne3meq5aSn9XLyUdCD", "george": "JBFqnCBsd6RMkjVDRZzb", "callum": "N2lVS1w4EtoT3dr4eOWO",
	"river": "SAz9YHcvj6GT2YYXdXww", "harry": "SOYHLrjzK2X1ezoPC6cr", "liam": "TX3LPaxmHKxFdv7VOQHJ",
	"alice": "Xb7hH8MSUJpSbSDYk0k2", "matilda": "XrExE9yKIg1WjnnlVkGX", "will": "bIHbv24MWmeRgasZH58o",
	"jessica": "cgSgspJ2msm6clMCkdW9", "eric": "cjVigY5qzO86Huf0OWal", "bella": "hpp4J3VqNfWAUOO0d1Us",
	"chris": "iP95p4xoKVk53GoZ742B", "brian": "nPczCjzI2devNBz1zQrb", "daniel": "onwK4e9ZLuTAKqWW03F9",
	"lily": "pFZP5JQG7iQjIQuC4Bku", "adam": "pNInz6obpgDQGcFmaJgB", "bill": "pqHfZKP75CvOlQylNhV4",
}

func VoiceID(voice string) (string, bool) {
	id, ok := voiceIDs[strings.ToLower(strings.TrimSpace(voice))]
	return id, ok
}

func VoiceNames() []string {
	return []string{"roger", "sarah", "laura", "charlie", "george", "callum", "river", "harry", "liam", "alice", "matilda", "will", "jessica", "eric", "bella", "chris", "brian", "daniel", "lily", "adam", "bill"}
}
