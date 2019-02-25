/*
The akismet package provides a client for using the Akismet API.

Usage:

	import "github.com/adtac/go-akismet/akismet"

Here's an example if you want to check whether a particular comment is spam or
not using the akismet.Check method:

	akismetKey := "abcdef012345"
	isSpam, err := akismet.Check(akismet.Comment{
		Blog: "https://example.com",                 // required
		UserIP: "8.8.8.8",                           // required
		UserAgent: "...",                            // required
		CommentType: "comment",
		CommentAuthor: "Billie Joe",
		CommentAuthorEmail: "billie@example.com",
		CommentContent: "Something's on my mind",
	}, akismetKey)

	if err != nil {
		// There was some issue with the API request. Most probable cause is
		// missing required fields.
	}

You can also submit false positives (comments that were wrongly marked as spam)
with the akismet.SubmitHam method. Or you can submit false negatives (comments
that should be marked as spam, but weren't) with the akismet.SubmitSpam method.
Both methods have the same method signature as the akismet.Check function: an
akismet.Comment structure as the first argument followed by your API key.
*/
package akismet
