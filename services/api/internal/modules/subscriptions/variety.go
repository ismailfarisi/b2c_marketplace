package subscriptions

import (
	"sort"
	"strings"
	"unicode"
)

// Variety levels, worst to best.
const (
	VarietyLow   = "low"
	VarietyGood  = "good"
	VarietyGreat = "great"
)

// Variety warning codes.
const (
	WarnSameCuisine         = "same_cuisine"
	WarnRepeatedDish        = "repeated_dish"
	WarnRepeatedAcrossWeeks = "repeated_across_weeks"
)

const maxVarietyWarnings = 6

// VarietyKitchen is one kitchen of a selection as the variety check sees it:
// its cuisines and the dishes it cooks during its rotation week.
type VarietyKitchen struct {
	Key      string
	Cuisines []string
	Dishes   []VarietyDish
}

// VarietyDish is one dish on one delivery weekday (0 = Sunday).
type VarietyDish struct {
	Weekday int
	Name    string
	NameAr  *string
}

// Variety is the advisory variety check of a rotation. It never blocks a purchase.
type Variety struct {
	Level            string           `json:"level"`
	Score            int              `json:"score"`
	DistinctCuisines int              `json:"distinct_cuisines"`
	Warnings         []VarietyWarning `json:"warnings"`
}

// VarietyWarning explains one thing that makes the selection repetitive.
type VarietyWarning struct {
	Code     string   `json:"code"`
	Cuisines []string `json:"cuisines,omitempty"`
	Dish     string   `json:"dish,omitempty"`
	DishAr   *string  `json:"dish_ar,omitempty"`
	Weekday  *int     `json:"weekday,omitempty"`
	Kitchens int      `json:"kitchens"`
}

// ScoreVariety scores a kitchen selection on distinct cuisines, the same dish
// on the same weekday at several kitchens, and dishes repeated across weeks.
func ScoreVariety(kitchens []VarietyKitchen) Variety {
	v := Variety{Level: VarietyGreat, Score: 100, Warnings: []VarietyWarning{}}
	n := len(kitchens)
	if n == 0 {
		return v
	}

	cuisineSets := make([][]string, n)
	for i, k := range kitchens {
		cuisineSets[i] = normalizeCuisines(k.Cuisines)
	}
	v.DistinctCuisines = distinctCuisines(cuisineSets)
	score := 100 - 20*(n-v.DistinctCuisines)
	if v.DistinctCuisines < n {
		v.Warnings = append(v.Warnings, sameCuisineWarning(cuisineSets))
	}

	dishWarnings, penalty := dishOverlap(kitchens)
	score -= penalty
	v.Warnings = append(v.Warnings, dishWarnings...)
	if len(v.Warnings) > maxVarietyWarnings {
		v.Warnings = v.Warnings[:maxVarietyWarnings]
	}

	v.Score = max(0, min(100, score))
	switch {
	// A mild cross-week repeat keeps "great"; the warning itself stays
	// visible so the customer can still act on it.
	case v.Score >= 85 && v.DistinctCuisines == n:
		v.Level = VarietyGreat
	case v.Score >= 55:
		v.Level = VarietyGood
	default:
		v.Level = VarietyLow
	}
	return v
}

func normalizeCuisines(in []string) []string {
	out := []string{}
	for _, c := range in {
		c = strings.ReplaceAll(strings.ToLower(strings.TrimSpace(c)), " ", "_")
		c = strings.ReplaceAll(c, "-", "_")
		if c != "" && !containsString(out, c) {
			out = append(out, c)
		}
	}
	return out
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// distinctCuisines is the most distinct cuisines the kitchens can represent
// when each kitchen counts for one of its cuisines. A kitchen with no cuisine
// listed counts as its own. Two kitchens tagged with the same set of cuisines
// are the same kind of food twice, not two cuisines, so identical sets are
// counted once before kitchens are matched to cuisines.
func distinctCuisines(sets [][]string) int {
	seen := map[string]bool{}
	uniq := make([][]string, 0, len(sets))
	for _, set := range sets {
		if len(set) == 0 {
			uniq = append(uniq, set)
			continue
		}
		sorted := append([]string(nil), set...)
		sort.Strings(sorted)
		key := strings.Join(sorted, "\x00")
		if seen[key] {
			continue
		}
		seen[key] = true
		uniq = append(uniq, set)
	}
	sets = uniq
	best := 0
	used := map[string]int{}
	var walk func(i, distinct int)
	walk = func(i, distinct int) {
		if distinct+len(sets)-i <= best {
			return
		}
		if i == len(sets) {
			best = distinct
			return
		}
		if len(sets[i]) == 0 {
			walk(i+1, distinct+1)
			return
		}
		for j, c := range sets[i] {
			if j >= 4 {
				break
			}
			used[c]++
			if used[c] == 1 {
				walk(i+1, distinct+1)
			} else {
				walk(i+1, distinct)
			}
			used[c]--
		}
	}
	walk(0, 0)
	return best
}

func sameCuisineWarning(sets [][]string) VarietyWarning {
	counts := map[string]int{}
	for _, set := range sets {
		for _, c := range set {
			counts[c]++
		}
	}
	shared := []string{}
	most := 1
	for c, k := range counts {
		if k >= 2 {
			shared = append(shared, c)
			most = max(most, k)
		}
	}
	sort.Slice(shared, func(i, j int) bool {
		if counts[shared[i]] != counts[shared[j]] {
			return counts[shared[i]] > counts[shared[j]]
		}
		return shared[i] < shared[j]
	})
	return VarietyWarning{Code: WarnSameCuisine, Cuisines: shared, Kitchens: most}
}

type dishGroup struct {
	name      string
	nameAr    *string
	first     int
	byWeekday map[int]map[int]bool
	kitchens  map[int]bool
}

func dishOverlap(kitchens []VarietyKitchen) ([]VarietyWarning, int) {
	type entry struct {
		kitchen int
		dish    VarietyDish
		en, ar  string
	}
	entries := []entry{}
	for ki, k := range kitchens {
		for _, d := range k.Dishes {
			e := entry{kitchen: ki, dish: d, en: NormalizeDishName(d.Name)}
			if d.NameAr != nil {
				e.ar = NormalizeDishName(*d.NameAr)
			}
			if e.en != "" || e.ar != "" {
				entries = append(entries, e)
			}
		}
	}

	parent := make([]int, len(entries))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	byEn, byAr := map[string]int{}, map[string]int{}
	for i, e := range entries {
		if e.en != "" {
			if j, ok := byEn[e.en]; ok {
				parent[find(i)] = find(j)
			} else {
				byEn[e.en] = i
			}
		}
		if e.ar != "" {
			if j, ok := byAr[e.ar]; ok {
				parent[find(i)] = find(j)
			} else {
				byAr[e.ar] = i
			}
		}
	}

	groups := map[int]*dishGroup{}
	for i, e := range entries {
		root := find(i)
		g := groups[root]
		if g == nil {
			g = &dishGroup{name: e.dish.Name, first: i, byWeekday: map[int]map[int]bool{}, kitchens: map[int]bool{}}
			groups[root] = g
		}
		if g.nameAr == nil && e.dish.NameAr != nil && strings.TrimSpace(*e.dish.NameAr) != "" {
			g.nameAr = e.dish.NameAr
		}
		if g.byWeekday[e.dish.Weekday] == nil {
			g.byWeekday[e.dish.Weekday] = map[int]bool{}
		}
		g.byWeekday[e.dish.Weekday][e.kitchen] = true
		g.kitchens[e.kitchen] = true
	}

	warnings := []VarietyWarning{}
	penalty := 0
	for _, g := range groups {
		sameDay := false
		for wd, ks := range g.byWeekday {
			if len(ks) < 2 {
				continue
			}
			sameDay = true
			day := wd
			warnings = append(warnings, VarietyWarning{
				Code: WarnRepeatedDish, Dish: g.name, DishAr: g.nameAr, Weekday: &day, Kitchens: len(ks),
			})
			penalty += 10 * (len(ks) - 1)
		}
		if !sameDay && len(g.kitchens) >= 2 {
			warnings = append(warnings, VarietyWarning{
				Code: WarnRepeatedAcrossWeeks, Dish: g.name, DishAr: g.nameAr, Kitchens: len(g.kitchens),
			})
			penalty += 5 * (len(g.kitchens) - 1)
		}
	}
	sort.SliceStable(warnings, func(i, j int) bool {
		a, b := warnings[i], warnings[j]
		if (a.Code == WarnRepeatedDish) != (b.Code == WarnRepeatedDish) {
			return a.Code == WarnRepeatedDish
		}
		if a.Kitchens != b.Kitchens {
			return a.Kitchens > b.Kitchens
		}
		if a.Dish != b.Dish {
			return a.Dish < b.Dish
		}
		return weekdayOf(a) < weekdayOf(b)
	})
	return warnings, penalty
}

func weekdayOf(w VarietyWarning) int {
	if w.Weekday == nil {
		return -1
	}
	return *w.Weekday
}

var dishStopwords = map[string]bool{
	"with": true, "and": true, "the": true, "of": true, "in": true, "w": true, "n": true, "a": true,
	"مع": true, "و": true, "في": true, "على": true,
}

// NormalizeDishName folds a dish name (English or Arabic) to a comparison key:
// case, punctuation, word order, plurals, Arabic diacritics, tatweel,
// alef/ya/ta-marbuta variants and the definite article are ignored.
func NormalizeDishName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r == 'ـ' || unicode.Is(unicode.Mn, r):
			continue
		case r == 'أ' || r == 'إ' || r == 'آ' || r == 'ٱ':
			r = 'ا'
		case r == 'ى' || r == 'ئ':
			r = 'ي'
		case r == 'ة':
			r = 'ه'
		case r == 'ؤ':
			r = 'و'
		case r >= '٠' && r <= '٩':
			r = '0' + (r - '٠')
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteRune(' ')
		}
	}
	words := []string{}
	for _, w := range strings.Fields(b.String()) {
		if dishStopwords[w] {
			continue
		}
		w = stemDishWord(w)
		if w != "" && !dishStopwords[w] && !containsString(words, w) {
			words = append(words, w)
		}
	}
	sort.Strings(words)
	return strings.Join(words, " ")
}

func stemDishWord(w string) string {
	runes := []rune(w)
	if len(runes) > 0 && runes[0] >= 0x0600 && runes[0] <= 0x06FF {
		for _, prefix := range []string{"بال", "وال", "ال"} {
			p := []rune(prefix)
			if len(runes)-len(p) >= 2 && string(runes[:len(p)]) == prefix {
				return string(runes[len(p):])
			}
		}
		return w
	}
	if len(runes) > 3 && strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") {
		if strings.HasSuffix(w, "ies") && len(runes) > 4 {
			return w[:len(w)-3] + "y"
		}
		return w[:len(w)-1]
	}
	return w
}
