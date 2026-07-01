package database

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"gorm.io/gorm"
)

var (
	// ErrSearchUnavailable means the binary was not built with SQLite FTS5 support.
	ErrSearchUnavailable = errors.New("full-text search is unavailable")
)

// PoemSearchFilter reuses the safe compound query filters for full-text search.
type PoemSearchFilter = PoemQueryFilter

// PoemSearchResult is a poem result with search metadata.
type PoemSearchResult struct {
	Poem      Poem              `json:"poem"`
	Rank      float64           `json:"rank"`
	HitFields []string          `json:"hit_fields"`
	Snippets  map[string]string `json:"snippets"`
}

// SQLiteFTS5Enabled reports whether this binary was built with -tags sqlite_fts5.
func SQLiteFTS5Enabled() bool {
	return sqliteFTS5Enabled
}

// RebuildPoemFTSIndex rebuilds the FTS index for this repository language.
func (r *Repository) RebuildPoemFTSIndex() error {
	if !sqliteFTS5Enabled {
		return fmt.Errorf("%w: rebuild with -tags sqlite_fts5", ErrSearchUnavailable)
	}

	poemTable := r.poemsTable()
	authorTable := r.authorsTable()
	ftsTable := r.poemFTSTable()

	type sourceRow struct {
		ID      int64  `gorm:"column:id"`
		Title   string `gorm:"column:title"`
		Content string `gorm:"column:content"`
		Author  string `gorm:"column:author"`
	}

	var rows []sourceRow
	err := r.db.Table(poemTable).
		Select(poemTable + ".id, " + poemTable + ".title, CAST(" + poemTable + ".content AS TEXT) AS content, " +
			"COALESCE(" + authorTable + ".name, '') AS author").
		Joins("LEFT JOIN " + authorTable + " ON " + poemTable + ".author_id = " + authorTable + ".id").
		Find(&rows).Error
	if err != nil {
		return err
	}

	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("DROP TABLE IF EXISTS " + ftsTable).Error; err != nil {
			return err
		}
		if err := tx.Exec("CREATE VIRTUAL TABLE " + ftsTable + " USING fts5(title, content, author)").Error; err != nil {
			return err
		}

		insertSQL := "INSERT INTO " + ftsTable + " (rowid, title, content, author) VALUES (?, ?, ?, ?)"
		for _, row := range rows {
			if err := tx.Exec(
				insertSQL,
				row.ID,
				tokenizeForFTS(row.Title),
				tokenizeForFTS(jsonTextForFTS(row.Content)),
				tokenizeForFTS(row.Author),
			).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// EnsurePoemFTSIndex populates the FTS table when it exists but is empty.
func (r *Repository) EnsurePoemFTSIndex() error {
	if !sqliteFTS5Enabled {
		return nil
	}

	var poemCount int64
	if err := r.db.Table(r.poemsTable()).Count(&poemCount).Error; err != nil {
		return err
	}
	if poemCount == 0 {
		return nil
	}

	var indexedCount int64
	if err := r.db.Table(r.poemFTSTable()).Count(&indexedCount).Error; err != nil {
		return err
	}
	if indexedCount > 0 {
		return nil
	}

	return r.RebuildPoemFTSIndex()
}

// SearchPoemsFTS searches poems through the SQLite FTS5 index and then applies
// the same safe filters used by compound query.
func (r *Repository) SearchPoemsFTS(filter PoemSearchFilter) ([]PoemSearchResult, int64, error) {
	if !sqliteFTS5Enabled {
		return nil, 0, fmt.Errorf("%w: rebuild with -tags sqlite_fts5", ErrSearchUnavailable)
	}

	keyword := strings.TrimSpace(filter.Keyword)
	if keyword == "" {
		return nil, 0, fmt.Errorf("%w: q is required", ErrInvalidQueryParam)
	}

	page := filter.Page
	if page < 1 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}

	searchIn := strings.ToLower(strings.TrimSpace(filter.SearchIn))
	if searchIn == "" {
		searchIn = "all"
	}
	if !isAllowedSearchIn(searchIn) {
		return nil, 0, fmt.Errorf("%w: unsupported search_in %q", ErrInvalidQueryParam, filter.SearchIn)
	}

	matchExpr, err := buildFTSMatchExpression(searchIn, keyword)
	if err != nil {
		return nil, 0, err
	}

	orderClause, err := poemSearchOrderClause(r.poemsTable(), filter.Sort)
	if err != nil {
		return nil, 0, err
	}

	poemTable := r.poemsTable()
	typeTable := r.poetryTypesTable()
	ftsTable := r.poemFTSTable()
	needsTypeJoin := filter.Lines != nil || filter.CharsPerLine != nil

	buildQuery := func() *gorm.DB {
		query := r.db.Table(poemTable).
			Joins("JOIN "+ftsTable+" ON "+poemTable+".id = "+ftsTable+".rowid").
			Where(ftsTable+" MATCH ?", matchExpr)

		if needsTypeJoin {
			query = query.Joins("LEFT JOIN " + typeTable + " ON " + poemTable + ".type_id = " + typeTable + ".id")
		}
		if filter.DynastyID != nil {
			query = query.Where(poemTable+".dynasty_id = ?", *filter.DynastyID)
		}
		if filter.AuthorID != nil {
			query = query.Where(poemTable+".author_id = ?", *filter.AuthorID)
		}
		if len(filter.TypeIDs) > 0 {
			query = query.Where(poemTable+".type_id IN ?", filter.TypeIDs)
		}
		if len(filter.TagIDs) > 0 {
			for i, tagID := range filter.TagIDs {
				alias := fmt.Sprintf("pt_match_%d", i)
				query = query.Where(
					fmt.Sprintf("EXISTS (SELECT 1 FROM poem_tags %s WHERE %s.poem_id = %s.id AND %s.tag_id = ?)", alias, alias, poemTable, alias),
					tagID,
				)
			}
		}
		if filter.Lines != nil {
			query = query.Where(typeTable+".lines = ?", *filter.Lines)
		}
		if filter.CharsPerLine != nil {
			query = query.Where(typeTable+".chars_per_line = ?", *filter.CharsPerLine)
		}

		return query
	}

	var total int64
	countQuery := buildQuery()
	if err := countQuery.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	type searchRow struct {
		Poem
		Rank float64 `gorm:"column:rank"`
	}

	var rows []searchRow
	offset := (page - 1) * pageSize
	err = buildQuery().
		Select(poemTable + ".*, bm25(" + ftsTable + ", 5.0, 3.0, 2.0) AS rank").
		Order(orderClause).
		Limit(pageSize).
		Offset(offset).
		Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}

	poems := make([]Poem, len(rows))
	for i := range rows {
		poems[i] = rows[i].Poem
	}
	r.loadPoemRelations(poems)

	results := make([]PoemSearchResult, len(rows))
	for i := range rows {
		results[i] = PoemSearchResult{
			Poem:      poems[i],
			Rank:      rows[i].Rank,
			HitFields: findPoemHitFields(poems[i], keyword),
			Snippets:  buildPoemSnippets(poems[i], keyword),
		}
	}

	return results, total, nil
}

func buildFTSMatchExpression(searchIn, keyword string) (string, error) {
	phrase := quoteFTS5Phrase(keyword)
	if phrase == "" {
		return "", fmt.Errorf("%w: q is required", ErrInvalidQueryParam)
	}

	switch searchIn {
	case "all":
		return phrase, nil
	case "title":
		return "title:" + phrase, nil
	case "content":
		return "content:" + phrase, nil
	case "author":
		return "author:" + phrase, nil
	default:
		return "", fmt.Errorf("%w: unsupported search_in %q", ErrInvalidQueryParam, searchIn)
	}
}

func poemSearchOrderClause(poemTable, sort string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(sort)) {
	case "", "relevance", "rank":
		return "rank ASC", nil
	case "id_desc":
		return poemTable + ".id DESC", nil
	case "id_asc":
		return poemTable + ".id ASC", nil
	case "title_asc":
		return poemTable + ".title ASC", nil
	case "title_desc":
		return poemTable + ".title DESC", nil
	default:
		return "", fmt.Errorf("%w: unsupported sort %q", ErrInvalidQueryParam, sort)
	}
}

func quoteFTS5Phrase(value string) string {
	tokens := tokenizeForFTS(value)
	if tokens == "" {
		return ""
	}
	return `"` + strings.ReplaceAll(tokens, `"`, `""`) + `"`
}

func tokenizeForFTS(value string) string {
	var tokens []string
	var ascii []rune

	flushASCII := func() {
		if len(ascii) == 0 {
			return
		}
		tokens = append(tokens, string(ascii))
		ascii = ascii[:0]
	}

	for _, r := range strings.ToLower(value) {
		switch {
		case unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r):
			flushASCII()
		case r <= unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			ascii = append(ascii, r)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			flushASCII()
			tokens = append(tokens, string(r))
		default:
			flushASCII()
		}
	}
	flushASCII()

	return strings.Join(tokens, " ")
}

func cleanFTSQuery(value string) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsPunct(r) || unicode.IsSymbol(r) {
			return ' '
		}
		return r
	}, value)
	return strings.Join(strings.Fields(value), " ")
}

func jsonTextForFTS(raw string) string {
	var lines []string
	if err := json.Unmarshal([]byte(raw), &lines); err == nil {
		return strings.Join(lines, "\n")
	}
	return raw
}

func poemContentText(poem Poem) string {
	return jsonTextForFTS(string(poem.Content))
}

func findPoemHitFields(poem Poem, keyword string) []string {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil
	}

	fields := make([]string, 0, 5)
	if strings.Contains(poem.Title, keyword) {
		fields = append(fields, "title")
	}
	if strings.Contains(poemContentText(poem), keyword) {
		fields = append(fields, "content")
	}
	if poem.Author != nil && strings.Contains(poem.Author.Name, keyword) {
		fields = append(fields, "author")
	}
	if poem.Dynasty != nil && strings.Contains(poem.Dynasty.Name, keyword) {
		fields = append(fields, "dynasty")
	}
	if poem.Type != nil && strings.Contains(poem.Type.Name, keyword) {
		fields = append(fields, "type")
	}
	return fields
}

func buildPoemSnippets(poem Poem, keyword string) map[string]string {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil
	}

	snippets := map[string]string{}
	if snippet := snippetAround(poem.Title, keyword, 12); snippet != "" {
		snippets["title"] = snippet
	}
	if snippet := snippetAround(poemContentText(poem), keyword, 18); snippet != "" {
		snippets["content"] = snippet
	}
	if poem.Author != nil {
		if snippet := snippetAround(poem.Author.Name, keyword, 12); snippet != "" {
			snippets["author"] = snippet
		}
	}
	if poem.Dynasty != nil {
		if snippet := snippetAround(poem.Dynasty.Name, keyword, 12); snippet != "" {
			snippets["dynasty"] = snippet
		}
	}
	if poem.Type != nil {
		if snippet := snippetAround(poem.Type.Name, keyword, 12); snippet != "" {
			snippets["type"] = snippet
		}
	}
	return snippets
}

func snippetAround(text, keyword string, context int) string {
	textRunes := []rune(text)
	keywordRunes := []rune(keyword)
	if len(textRunes) == 0 || len(keywordRunes) == 0 {
		return ""
	}

	idx := indexRunes(textRunes, keywordRunes)
	if idx < 0 {
		return ""
	}

	start := max(0, idx-context)
	end := min(len(textRunes), idx+len(keywordRunes)+context)
	prefix := ""
	if start > 0 {
		prefix = "..."
	}
	suffix := ""
	if end < len(textRunes) {
		suffix = "..."
	}
	return prefix + string(textRunes[start:end]) + suffix
}

func indexRunes(text, keyword []rune) int {
	if len(keyword) > len(text) {
		return -1
	}
	for i := 0; i <= len(text)-len(keyword); i++ {
		matched := true
		for j := range keyword {
			if text[i+j] != keyword[j] {
				matched = false
				break
			}
		}
		if matched {
			return i
		}
	}
	return -1
}
