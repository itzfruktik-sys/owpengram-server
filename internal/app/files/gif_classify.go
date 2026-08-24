package files

import "strings"

// gifCategoryKeywords maps each domain.GifCatalogCategories entry to a list
// of lowercase words/phrases whose presence in a GIF title is decent
// (imperfect, but reversible -- an operator can always correct it in the
// admin panel) evidence the GIF belongs to that emotional category. Checked
// in this order, first match wins, so more specific/less ambiguous
// categories are listed before catch-alls like Neutral/Silly.
var gifCategoryKeywords = map[string][]string{
	"Love": {
		"love", "heart", "kiss", "kissing", "crush", "romance", "romantic",
		"valentine", "adorable", "hug", "hugging", "xoxo", "sweetheart",
	},
	"Cheers": {
		"party", "celebrate", "celebration", "celebrating", "woohoo", "cheers",
		"congrats", "congratulations", "dance", "dancing", "yay", "hooray",
		"victory", "champagne", "toast", "birthday", "win", "winning",
	},
	"Laughter": {
		"laugh", "laughing", "lol", "lmao", "haha", "hilarious", "funny",
		"joke", "giggle", "chuckle", "rofl", "lulz",
	},
	"Astonishment": {
		"shock", "shocked", "shocking", "omg", "wow", "surprised", "surprise",
		"gasp", "whoa", "unbelievable", "stunned", "speechless", "mindblown",
		"mind blown", "jawdrop",
	},
	"Sadness": {
		"sad", "sadness", "cry", "crying", "cries", "tears", "depressed",
		"heartbroken", "sorrow", "sob", "sobbing", "disappointed", "unhappy",
	},
	"Anger": {
		"angry", "anger", "mad", "rage", "furious", "pissed", "annoyed",
		"irritated", "punch", "smash", "yell", "yelling", "scream", "fist",
	},
	"Disapproval": {
		"disgusting", "disgusted", "gross", "eww", "yuck", "ugh", "cringe",
		"boo", "nope", "fail",
	},
	"Approval": {
		"agree", "thumbsup", "thumbs up", "nailed it", "well done",
		"applause", "clap", "clapping", "bravo", "approved", "salute",
		"respect", "nice one",
	},
	"Doubt": {
		"confused", "confusion", "suspicious", "skeptical", "hmm", "hmmm",
		"thinking", "uncertain", "unsure", "really",
	},
	"Silly": {
		"silly", "goofy", "derp", "wacky", "ridiculous", "nonsense",
	},
	"Neutral": {
		"meh", "whatever", "shrug", "indifferent", "blank stare", "boring",
	},
}

// ClassifyGifCategory guesses one of domain.GifCatalogCategories from a GIF's
// title via keyword matching, or "" if nothing matches. See
// gifCategoryKeywords for the word lists and Service.AdminAutoCategorizeGifCatalog
// for how this gets applied in bulk.
//
// This is deliberately simple substring matching, not NLP: real GIF titles
// range from descriptive ("Sad Tenant") to meaningless filename fragments
// ("jIDL92q") that no text-based heuristic can classify -- those are left
// uncategorized for an operator to tag by hand rather than guessed wrong.
func ClassifyGifCategory(title string) string {
	lower := strings.ToLower(title)
	for _, category := range gifCategoryOrder {
		for _, kw := range gifCategoryKeywords[category] {
			if strings.Contains(lower, kw) {
				return category
			}
		}
	}
	return ""
}

// gifCategoryOrder is gifCategoryKeywords' check order (Go map iteration is
// unordered, and the order is meaningful -- see gifCategoryKeywords' doc).
var gifCategoryOrder = []string{
	"Love", "Cheers", "Laughter", "Astonishment", "Sadness", "Anger",
	"Disapproval", "Approval", "Doubt", "Silly", "Neutral",
}
