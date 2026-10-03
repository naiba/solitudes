package model

import "gorm.io/gorm"

// articleTree is a metadata-only adjacency traversal. The supplied visibility
// scope is applied at every level, so an unreadable subtree cannot leak titles
// or counters. UNION terminates even if old data contains a parent cycle.
type articleTreeRow struct {
	Article
	RootID string
}

func articleTreeQuery(db *gorm.DB, roots []string, fields, result string) *gorm.DB {
	visible := db.Session(&gorm.Session{}).Model(&Article{}).
		Select(fields)
	return db.Session(&gorm.Session{NewDB: true}).Raw(`WITH RECURSIVE visible AS NOT MATERIALIZED (?), tree AS (
	 SELECT visible.id AS root_id, visible.* FROM visible WHERE id IN ?
	 UNION
	 SELECT tree.root_id, child.* FROM visible AS child JOIN tree ON child.book_refer=tree.id WHERE tree.is_book
	) `+result, visible, roots)
}

func articleTree(db *gorm.DB, roots []string) ([]articleTreeRow, error) {
	var rows []articleTreeRow
	err := articleTreeQuery(db, roots,
		"id, book_refer, is_book, title, slug, visibility, author_id, created_at, read_num, comment_num",
		"SELECT * FROM tree ORDER BY created_at, id").Scan(&rows).Error
	return rows, err
}

// AggregateBookCounts batches a page's counters instead of querying every book
// and sub-book separately. It replaces totals rather than double-counting them.
func AggregateBookCounts(db *gorm.DB, articles []Article) error {
	var roots []string
	positions := map[string]int{}
	for i := range articles {
		if articles[i].IsBook {
			roots = append(roots, articles[i].ID)
			positions[articles[i].ID] = i
		}
	}
	if len(roots) == 0 {
		return nil
	}
	var rows []struct {
		RootID              string
		ReadNum, CommentNum uint
	}
	err := articleTreeQuery(db, roots, "id, book_refer, is_book, read_num, comment_num",
		"SELECT root_id, SUM(read_num) AS read_num, SUM(comment_num) AS comment_num FROM tree GROUP BY root_id").Scan(&rows).Error
	if err != nil {
		return err
	}
	for _, i := range positions {
		articles[i].ReadNum = 0
		articles[i].CommentNum = 0
	}
	for _, row := range rows {
		i := positions[row.RootID]
		articles[i].ReadNum += row.ReadNum
		articles[i].CommentNum += row.CommentNum
	}
	return nil
}

// LoadChapters fetches no chapter bodies and builds the complete visible tree
// with one SQL query. The caller's root content and version stay untouched.
func (a *Article) LoadChapters(db *gorm.DB) error {
	a.Chapters = nil
	if !a.IsBook {
		return nil
	}
	rows, err := articleTree(db, []string{a.ID})
	if err != nil {
		return err
	}
	nodes := map[string]*Article{a.ID: a}
	for i := range rows {
		n := &rows[i].Article
		if n.ID == a.ID {
			a.ReadNum = n.ReadNum
			a.CommentNum = n.CommentNum
			continue
		}
		nodes[n.ID] = n
	}
	for i := range rows {
		n := &rows[i].Article
		if n.ID == a.ID || n.BookRefer == nil {
			continue
		}
		if parent := nodes[*n.BookRefer]; parent != nil {
			parent.Chapters = append(parent.Chapters, n)
		}
		a.ReadNum += n.ReadNum
		a.CommentNum += n.CommentNum
	}
	return nil
}
