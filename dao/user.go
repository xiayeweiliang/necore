package dao

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"necore/config"
	"necore/database"
	"necore/model"
	"slices"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// Hash

func checkPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

func hashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	return string(bytes), err
}

func DebugTestPassword() {
	password := "test"
	hash, _ := hashPassword(password)
	log.Println(`Test Password "test":`, hash)
}

func UnitTestPassword() string {
	hash, _ := hashPassword("unit-test-password")
	return hash
}

// Token

func CreateToken(u model.User) (string, error) {
	token := jwt.New(jwt.SigningMethodHS256)
	claims := token.Claims.(jwt.MapClaims)

	claims["name"] = u.Username
	claims["ver"] = u.TokenVersion
	claims["iat"] = time.Now().Unix()
	claims["exp"] = time.Now().Add(time.Hour * 72).Unix()

	t, err := token.SignedString([]byte(config.Config("SECRET")))
	return t, err
}

func ContainsGroup(userGroup string, group string) bool {
	var groups []string
	err := json.Unmarshal([]byte(userGroup), &groups)
	if err != nil {
		groups = []string{}
	}
	return slices.Contains(groups, group)
}

// Database

func GetUserByUsername(u string) (*model.User, error) {
	db := database.GetUserDatabase()
	var user model.User
	if err := db.Where(&model.User{Username: u}).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	return &user, nil
}

func AddUserByUsername(username string, password string) error {
	username = strings.TrimSpace(username)

	if username == "" {
		return fmt.Errorf("username cannot be empty")
	}

	if password == "" {
		return fmt.Errorf("password cannot be empty")
	}

	hash, err := hashPassword(password)
	if err != nil {
		return err
	}

	db := database.GetUserDatabase()

	var existingUser model.User

	err = db.Unscoped().
		Where("username = ?", username).
		First(&existingUser).Error

	if err == nil {
		if !existingUser.DeletedAt.Valid {
			return fmt.Errorf("user already exists")
		}

		// 这里不要新建用户，而是复活旧用户。
		// 关键点：token_version 必须递增，避免旧 JWT 在同名用户重建后重新有效。
		return db.Unscoped().
			Model(&existingUser).
			Updates(map[string]any{
				"password":      hash,
				"group":         `[]`,
				"tags":          `[]`,
				"avatar":        "",
				"token_version": gorm.Expr("token_version + ?", 1),
				"deleted_at":    nil,
				"updated_at":    time.Now(),
			}).Error
	}

	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	user := model.User{
		Username:     username,
		Password:     hash,
		Group:        `[]`,
		Tags:         `[]`,
		Avatar:       "",
		TokenVersion: 1,
	}

	return db.Create(&user).Error
}

func GetAllUsers() ([]model.User, error) {
	db := database.GetUserDatabase()
	var users []model.User
	err := db.Find(&users).Error
	return users, err
}

func DeleteUserByUsername(username string) error {
	db := database.GetUserDatabase()

	result := db.
		Where("username = ?", username).
		Delete(&model.User{})

	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("user '%s' not found", username)
	}

	return nil
}

func CheckUserPassword(input string, password string) bool {
	return checkPasswordHash(input, password)
}

func UpdateUserPassword(username string, password string) error {
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}

	db := database.GetUserDatabase()
	var user *model.User
	db.Where(&model.User{Username: username}).First(&user)
	return db.Model(&user).Updates(model.User{Password: hash}).Error
}

func UpdateUserInfo(username string, group string, tags string) error {
	db := database.GetUserDatabase()
	var user *model.User
	db.Where(&model.User{Username: username}).First(&user)
	return db.Model(&user).Updates(model.User{Group: group, Tags: tags}).Error
}

func UpdateUserPermissions(username string) error {
	db := database.GetUserDatabase()

	return db.Model(&model.User{}).
		Where(model.User{Username: username}).
		UpdateColumn(
			"token_version",
			gorm.Expr("token_version + ?", 1),
		).Error
}

func GetUserAvatar(username string) (string, error) {
	db := database.GetUserDatabase()
	var user model.User
	if err := db.Where(&model.User{Username: username}).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}

	return user.Avatar, nil
}

func UpdateUserAvatar(username string, avatar string) error {
	db := database.GetUserDatabase()
	var user *model.User
	db.Where(&model.User{Username: username}).First(&user)
	return db.Model(&user).Updates(model.User{Avatar: avatar}).Error
}
