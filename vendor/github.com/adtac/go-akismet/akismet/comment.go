package akismet

type Comment struct {
	Blog               string `form:"blog"`
	UserIP             string `form:"user_ip"`
	UserAgent          string `form:"user_agent"`
	Referrer           string `form:"referrer"`
	Permalink          string `form:"permalink"`
	CommentType        string `form:"comment_type"`
	CommentAuthor      string `form:"comment_author"`
	CommentAuthorEmail string `form:"comment_author_email"`
	CommentAuthorURL   string `form:"comment_author_url"`
	CommentContent     string `form:"comment_content"`
	BlogLang           string `form:"blog_lang"`
	BlogCharset        string `form:"blog_charset"`
	UserRole           string `form:"user_role"`
	// TODO: Add support for comment_date_gmt and comment_post_modified_gmt
}
