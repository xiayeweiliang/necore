package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"necore/dao"
	"necore/model"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// Handlers

const dummyPasswordHash = "$2a$12$KIX5gnQp2zfN8mjxJZ3v/eJQJjMKqQn2Cw4dHpRrv0vJ6X6WfR4he"

func Login(c *fiber.Ctx) error {
	// Parse Request Body
	type LoginInput struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	input := new(LoginInput)
	if err := c.BodyParser(&input); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Error on login request", "err": err})
	}

	// Get User
	userModel, err := dao.GetUserByUsername(input.Username)

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Internal Server Error", "err": err})
	} else if userModel == nil {
		// Avoid timing attack
		dao.CheckUserPassword(input.Password, dummyPasswordHash)
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid identity or password", "err": err})
	}

	// Check Password
	if !dao.CheckUserPassword(input.Password, userModel.Password) {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid identity or password"})
	}

	// Token
	t, err := dao.CreateToken(*userModel)
	if err != nil {
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	// User Info
	type TagEntity struct {
		Text     string `json:"text"`
		Color    string `json:"color"`
		TagColor string `json:"tagColor"`
	}
	type UserInfo struct {
		Username string      `json:"username"`
		Group    []string    `json:"group"`
		Tags     []TagEntity `json:"tags"`
	}

	var groups []string
	err = json.Unmarshal([]byte(userModel.Group), &groups)
	if err != nil {
		groups = []string{}
	}
	var tags []TagEntity
	err = json.Unmarshal([]byte(userModel.Tags), &tags)
	if err != nil {
		tags = []TagEntity{}
	}
	userInfo := UserInfo{
		Username: userModel.Username,
		Group:    groups,
		Tags:     tags,
	}
	return c.JSON(fiber.Map{
		"token": t,
		"user":  userInfo,
	})
}

// Register by admin
func AddUser(c *fiber.Ctx) error {
	currentUser := c.Locals("currentUser").(model.User)

	if !dao.ContainsGroup(currentUser.Group, "admin") {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": "Forbidden",
		})
	}

	type NewUser struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	user := new(NewUser)

	if err := c.BodyParser(user); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Review your input",
			"err":   err.Error(),
		})
	}

	username := strings.TrimSpace(user.Username)

	if username == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Username cannot be empty",
		})
	}

	if user.Password == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Password cannot be empty",
		})
	}

	if err := dao.AddUserByUsername(username, user.Password); err != nil {
		if errors.Is(err, fmt.Errorf("user already exists")) {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{
				"error": "Username already exists",
			})
		}

		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(fiber.Map{})
}

func GetStatus(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"status": "alive"})
}
