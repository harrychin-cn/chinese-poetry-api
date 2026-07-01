package database

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Tag describes a value-added label such as theme, mood, grade, or scenario.
type Tag struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Category    string    `json:"category"`
	Description string    `json:"description,omitempty"`
	Source      string    `json:"source"`
	CreatedAt   time.Time `json:"created_at"`
}

// PoemTag links poems to tags.
type PoemTag struct {
	ID        int64     `json:"id"`
	PoemID    int64     `json:"poem_id"`
	TagID     int64     `json:"tag_id"`
	CreatedAt time.Time `json:"created_at"`
}

// TagInput is used for importing and assigning tags.
type TagInput struct {
	Name        string `json:"name"`
	Category    string `json:"category"`
	Description string `json:"description"`
	Source      string `json:"source"`
}

// UpsertTag creates or updates a tag.
func (r *Repository) UpsertTag(input TagInput) (*Tag, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: tag name is required", ErrInvalidQueryParam)
	}

	category := strings.TrimSpace(input.Category)
	if category == "" {
		category = "theme"
	}

	source := strings.TrimSpace(input.Source)
	if source == "" {
		source = "manual"
	}

	tag := &Tag{
		Name:        name,
		Category:    category,
		Description: strings.TrimSpace(input.Description),
		Source:      source,
		CreatedAt:   time.Now().UTC(),
	}

	var existing Tag
	err := r.db.Table("tags").Where("name = ? AND category = ?", name, category).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if err := r.db.Table("tags").Create(tag).Error; err != nil {
			return nil, err
		}
		return tag, nil
	}
	if err != nil {
		return nil, err
	}

	updates := map[string]any{
		"description": tag.Description,
		"source":      tag.Source,
	}
	if err := r.db.Table("tags").Where("id = ?", existing.ID).Updates(updates).Error; err != nil {
		return nil, err
	}

	existing.Description = tag.Description
	existing.Source = tag.Source
	return &existing, nil
}

// ListTags lists all tags, optionally filtered by category.
func (r *Repository) ListTags(category string) ([]Tag, error) {
	var tags []Tag
	query := r.db.Table("tags")
	if category = strings.TrimSpace(category); category != "" {
		query = query.Where("category = ?", category)
	}
	err := query.Order("category ASC, name ASC").Find(&tags).Error
	return tags, err
}

// ListTagsByPoemIDs returns tags grouped by poem ID. It is used by knowledge/RAG
// responses so clients can consume a poem together with its value-added labels.
func (r *Repository) ListTagsByPoemIDs(poemIDs []int64) (map[int64][]Tag, error) {
	result := make(map[int64][]Tag, len(poemIDs))
	if len(poemIDs) == 0 {
		return result, nil
	}

	var rows []struct {
		PoemID      int64
		ID          int64
		Name        string
		Category    string
		Description string
		Source      string
		CreatedAt   time.Time
	}
	if err := r.db.Table("poem_tags").
		Select("poem_tags.poem_id, tags.id, tags.name, tags.category, tags.description, tags.source, tags.created_at").
		Joins("JOIN tags ON tags.id = poem_tags.tag_id").
		Where("poem_tags.poem_id IN ?", poemIDs).
		Order("poem_tags.poem_id ASC, tags.category ASC, tags.name ASC").
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	for _, row := range rows {
		result[row.PoemID] = append(result[row.PoemID], Tag{
			ID:          row.ID,
			Name:        row.Name,
			Category:    row.Category,
			Description: row.Description,
			Source:      row.Source,
			CreatedAt:   row.CreatedAt,
		})
	}
	return result, nil
}

// AssignTagsToPoem assigns tags to a poem. Existing assignments are kept.
func (r *Repository) AssignTagsToPoem(poemID int64, inputs []TagInput) ([]Tag, error) {
	if poemID < 1 {
		return nil, fmt.Errorf("%w: poem_id must be positive", ErrInvalidQueryParam)
	}
	if len(inputs) == 0 {
		return []Tag{}, nil
	}

	var poemExists int64
	if err := r.db.Table(r.poemsTable()).Where("id = ?", poemID).Count(&poemExists).Error; err != nil {
		return nil, err
	}
	if poemExists == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	tags := make([]Tag, 0, len(inputs))
	err := r.db.Transaction(func(tx *gorm.DB) error {
		txRepo := &Repository{db: &DB{DB: tx}, lang: r.lang}
		for _, input := range inputs {
			tag, err := txRepo.UpsertTag(input)
			if err != nil {
				return err
			}

			assignment := &PoemTag{
				PoemID:    poemID,
				TagID:     tag.ID,
				CreatedAt: time.Now().UTC(),
			}
			if err := tx.Table("poem_tags").Where("poem_id = ? AND tag_id = ?", poemID, tag.ID).FirstOrCreate(assignment).Error; err != nil {
				return err
			}
			tags = append(tags, *tag)
		}
		return nil
	})

	if err != nil {
		return nil, err
	}
	return tags, nil
}

// QueryPoemsByTags queries poems that match all provided tags, then applies normal compound filters.
func (r *Repository) QueryPoemsByTags(filter PoemQueryFilter, tags []TagInput) ([]Poem, int64, error) {
	tagIDs, err := r.ResolveTagIDsForQuery(tags)
	if err != nil {
		return nil, 0, err
	}
	filter.TagIDs = tagIDs
	return r.QueryPoems(filter)
}

// ResolveTagIDsForQuery resolves tag names/categories into IDs for query handlers.
func (r *Repository) ResolveTagIDsForQuery(tags []TagInput) ([]int64, error) {
	if len(tags) == 0 {
		return nil, nil
	}

	ids := make([]int64, 0, len(tags))
	seen := make(map[int64]bool, len(tags))
	for _, tagInput := range tags {
		name := strings.TrimSpace(tagInput.Name)
		if name == "" {
			continue
		}
		category := strings.TrimSpace(tagInput.Category)

		var tag Tag
		query := r.db.Table("tags").Where("name = ?", name)
		if category != "" {
			query = query.Where("category = ?", category)
		}
		err := query.First(&tag).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: tag not found %q", ErrInvalidQueryParam, name)
		}
		if err != nil {
			return nil, err
		}
		if seen[tag.ID] {
			continue
		}
		seen[tag.ID] = true
		ids = append(ids, tag.ID)
	}

	return ids, nil
}

// ResolveExistingTagIDsForQuery resolves tag names/categories but ignores tags
// that are not present yet. This keeps scenario-based knowledge recall useful
// before the AI tagging dataset is fully populated.
func (r *Repository) ResolveExistingTagIDsForQuery(tags []TagInput) ([]int64, []Tag, error) {
	if len(tags) == 0 {
		return nil, nil, nil
	}

	ids := make([]int64, 0, len(tags))
	resolved := make([]Tag, 0, len(tags))
	seen := make(map[int64]bool, len(tags))
	for _, tagInput := range tags {
		name := strings.TrimSpace(tagInput.Name)
		if name == "" {
			continue
		}
		category := strings.TrimSpace(tagInput.Category)

		var tag Tag
		query := r.db.Table("tags").Where("name = ?", name)
		if category != "" {
			query = query.Where("category = ?", category)
		}
		err := query.First(&tag).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		if seen[tag.ID] {
			continue
		}
		seen[tag.ID] = true
		ids = append(ids, tag.ID)
		resolved = append(resolved, tag)
	}

	return ids, resolved, nil
}
