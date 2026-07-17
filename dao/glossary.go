package dao

import (
	"fmt"
	"necore/database"
	"necore/model"
)

func GetGlossaryList() ([]model.Glossary, error) {
	db := database.GetWikiDatabase()
	var list []model.Glossary
	err := db.Find(&list).Error
	return list, err
}

func GetGlossaryById(id string) (model.Glossary, error) {
	db := database.GetWikiDatabase()
	var entry model.Glossary
	err := db.Where("id = ?", id).First(&entry).Error
	return entry, err
}

func CreateGlossary(entry model.Glossary) error {
	db := database.GetWikiDatabase()
	return db.Create(&entry).Error
}

func UpdateGlossary(entry model.Glossary) error {
	result := database.GetWikiDatabase().
		Model(&model.Glossary{}).
		Where("id = ?", entry.Id).
		Updates(map[string]any{
			"name":    entry.Name,
			"type":    entry.Type,
			"gallery": entry.Gallery,
			"content": entry.Content,
		})

	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("glossary not found")
	}
	return nil
}

func DeleteGlossary(id string) error {
	result := database.GetWikiDatabase().
		Unscoped().
		Where("id = ?", id).
		Delete(&model.Glossary{})

	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("glossary not found")
	}
	return nil
}
