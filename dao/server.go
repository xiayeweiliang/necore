package dao

import (
	"fmt"
	"necore/database"
	"necore/model"
)

func GetServerList() ([]model.Server, error) {
	db := database.GetServerDatabase()
	var servers []model.Server
	err := db.Find(&servers).Error
	return servers, err
}

func AddServer(server model.Server) error {
	db := database.GetServerDatabase()
	return db.Create(&server).Error
}

func UpdateServer(server model.Server) error {
	result := database.GetServerDatabase().
		Model(&model.Server{}).
		Where("id = ?", server.Id).
		Updates(map[string]any{
			"name":           server.Name,
			"icon":           server.Icon,
			"description":    server.Description,
			"realtime":       server.Realtime,
			"online_map_url": server.OnlineMapUrl,
			"server_url":     server.ServerUrl,
		})

	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("server not found")
	}

	return nil
}

func DeleteServer(id string) error {
	result := database.GetServerDatabase().
		Unscoped().
		Where("id = ?", id).
		Delete(&model.Server{})

	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("server not found")
	}

	return nil
}
