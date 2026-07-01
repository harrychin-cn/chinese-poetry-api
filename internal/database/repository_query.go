package database

import (
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// ErrInvalidQueryParam is returned when an advanced query parameter is invalid.
var ErrInvalidQueryParam = errors.New("invalid query parameter")

// PoemQueryFilter contains the safe, whitelisted filters supported by the
// advanced poem query endpoint. It deliberately does not accept raw SQL.
type PoemQueryFilter struct {
	Keyword      string
	SearchIn     string
	DynastyID    *int64
	AuthorID     *int64
	TypeIDs      []int64
	TagIDs       []int64
	Lines        *int
	CharsPerLine *int
	Page         int
	PageSize     int
	Sort         string
}

// QueryPoems runs a safe compound query against poems.
func (r *Repository) QueryPoems(filter PoemQueryFilter) ([]Poem, int64, error) {
	poemTable := r.poemsTable()
	authorTable := r.authorsTable()
	typeTable := r.poetryTypesTable()

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

	orderClause, err := poemQueryOrderClause(poemTable, filter.Sort)
	if err != nil {
		return nil, 0, err
	}

	keyword := strings.TrimSpace(filter.Keyword)
	needsAuthorJoin := keyword != "" && (searchIn == "all" || searchIn == "author")
	needsTypeJoin := filter.Lines != nil || filter.CharsPerLine != nil
	needsTagJoin := len(filter.TagIDs) > 0

	buildQuery := func() *gorm.DB {
		query := r.db.Table(poemTable)

		if needsAuthorJoin {
			query = query.Joins("LEFT JOIN " + authorTable + " ON " + poemTable + ".author_id = " + authorTable + ".id")
		}
		if needsTypeJoin {
			query = query.Joins("LEFT JOIN " + typeTable + " ON " + poemTable + ".type_id = " + typeTable + ".id")
		}
		if needsTagJoin {
			query = query.Joins("JOIN poem_tags ON " + poemTable + ".id = poem_tags.poem_id")
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
			query = query.Where("poem_tags.tag_id IN ?", filter.TagIDs).
				Group(poemTable+".id").
				Having("COUNT(DISTINCT poem_tags.tag_id) = ?", len(filter.TagIDs))
		}
		if filter.Lines != nil {
			query = query.Where(typeTable+".lines = ?", *filter.Lines)
		}
		if filter.CharsPerLine != nil {
			query = query.Where(typeTable+".chars_per_line = ?", *filter.CharsPerLine)
		}

		if keyword != "" {
			pattern := "%" + keyword + "%"
			switch searchIn {
			case "title":
				query = query.Where(poemTable+".title LIKE ?", pattern)
			case "content":
				query = query.Where(poemTable+".content LIKE ?", pattern)
			case "author":
				query = query.Where(authorTable+".name LIKE ?", pattern)
			case "all":
				query = query.Where(
					"("+poemTable+".title LIKE ? OR "+poemTable+".content LIKE ? OR "+authorTable+".name LIKE ?)",
					pattern,
					pattern,
					pattern,
				)
			}
		}

		return query
	}

	var total int64
	countQuery := buildQuery()
	if len(filter.TagIDs) > 0 {
		if err := r.db.Table("(?) AS matched_poems", countQuery.Select(poemTable+".id")).Count(&total).Error; err != nil {
			return nil, 0, err
		}
	} else if err := countQuery.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var poems []Poem
	offset := (page - 1) * pageSize
	dataQuery := buildQuery()
	if err := dataQuery.
		Select(poemTable + ".*").
		Order(orderClause).
		Limit(pageSize).
		Offset(offset).
		Find(&poems).Error; err != nil {
		return nil, 0, err
	}

	r.loadPoemRelations(poems)
	return poems, total, nil
}

func isAllowedSearchIn(searchIn string) bool {
	switch searchIn {
	case "all", "title", "content", "author":
		return true
	default:
		return false
	}
}

func poemQueryOrderClause(poemTable, sort string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(sort)) {
	case "", "id_desc":
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
