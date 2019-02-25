package akismet

// This function submits a false positive to Akismet using the API. False
// positives are those comments that should *not* be marked as spam, but were
// accidentally. This method is mandatorily required in all implementations.
// If the request went fine, a nil error is returned. Otherwise the returned
// error is non-nil.
func SubmitHam(c *Comment, key string) error {
	_, err := postRequest(c, key, "submit-ham")
	return err
}

// This function submits a false negatives to Akismet using the API. False
// negatives are those comments that should be marked as spam, but were *not*
// accidentally. This method is mandatorily required in all implementations.
// If the request went fine, a nil error is returned. Otherwise the returned
// error is non-nil.
func SubmitSpam(c *Comment, key string) error {
	_, err := postRequest(c, key, "submit-spam")
	return err
}
