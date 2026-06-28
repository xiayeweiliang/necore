package dao

import (
	"fmt"
	"necore/database"
	"necore/model"
	"os"
)

// Database

func CreateArticle(id string) error {
	db := database.GetArticleDatabase()
	article := model.Article{
		Id: id,
	}
	return db.Create(&article).Error
}

func UpdateArticle(updatedArticle model.Article) error {
	result := database.GetArticleDatabase().
		Model(&model.Article{}).
		Where("id = ?", updatedArticle.Id).
		Updates(map[string]any{
			"pin":      updatedArticle.Pin,
			"title":    updatedArticle.Title,
			"brief":    updatedArticle.Brief,
			"date":     updatedArticle.Date,
			"end_date": updatedArticle.EndDate,
			"image":    updatedArticle.Image,
			"content":  updatedArticle.Content,
			"author":   updatedArticle.Author,
			"category": updatedArticle.Category,
		})

	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("Article not found")
	}

	return nil
}

func GetArticle(id string) (*model.Article, error) {
	db := database.GetArticleDatabase()
	article := model.Article{}
	if err := db.Where(&model.Article{Id: id}).First(&article).Error; err != nil {
		return nil, err
	}
	return &article, nil
}

func GetArticleCountByCategory(category string) (int64, error) {
	db := database.GetArticleDatabase()
	var count int64
	if err := db.Model(&model.Article{}).Where(&model.Article{Category: category}).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func GetArticleList(target string, page int, pageSize int, pin bool) ([]model.Article, error) {
	db := database.GetArticleDatabase()
	var articles []model.Article
	var err error
	if pin {
		err = db.Where(&model.Article{Category: target, Pin: pin}).
			Order("date desc").
			Offset((page - 1) * pageSize).
			Limit(pageSize).
			Find(&articles).Error
	} else {
		// return all articles including pinned and unpinned
		err = db.Where(&model.Article{Category: target}).
			Order("date desc").
			Offset((page - 1) * pageSize).
			Limit(pageSize).
			Find(&articles).Error
	}

	if err != nil {
		return nil, err
	}
	return articles, nil
}

func DeleteArticle(id string) error {
	db := database.GetArticleDatabase()

	result := db.Unscoped().
		Where("id = ?", id).
		Delete(&model.Article{})
	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("Article not found")
	}

	_ = os.RemoveAll(fmt.Sprintf("./contents/%s", id))
	return nil
}
