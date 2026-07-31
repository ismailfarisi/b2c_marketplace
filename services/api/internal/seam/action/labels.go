package action

// Override applies a stored action's own words over a handler's defaults, one
// language at a time.
//
// # The bug this replaces
//
// Every handler in the product wrote the same three lines:
//
//	label, labelAr := "Enquire", "استفسر"
//	if a.Label != "" {
//	    label, labelAr = a.Label, a.LabelAr
//	}
//
// which is all-or-nothing. A row that sets an English label and leaves the
// Arabic one blank — which is every row created by a seed, an admin tool, or
// anyone who does not speak both — replaced a perfectly good "استفسر" with an
// empty string. The CTA then rendered as an unlabelled orange bar in an Arabic
// build, or fell back to English if the client was lenient.
//
// Nine handlers had the identical mistake, which is the argument for this being
// one function rather than nine fixes.
//
// Per-language override is also the behaviour anyone would expect if asked: a
// vendor who renames their button in Arabic has not asked for the English one
// to disappear.
func Override(defaultLabel, defaultLabelAr, label, labelAr string) (string, string) {
	if label != "" {
		defaultLabel = label
	}
	if labelAr != "" {
		defaultLabelAr = labelAr
	}
	return defaultLabel, defaultLabelAr
}
