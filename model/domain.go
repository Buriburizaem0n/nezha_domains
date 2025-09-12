// src/model/domain.go (已修正)

package model

import (
	"time"

	"gorm.io/datatypes"
)

// Domain 是数据库中 domains 表的 GORM 模型
type Domain struct {
	ID          uint64 `gorm:"primaryKey"`
	Domain      string
	Status      string `gorm:"size:20;default:'pending'"`
	VerifyToken string
	IsPublic    bool           `gorm:"default:true"`
	BillingData datatypes.JSON `gorm:"type:json"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// BillingDataMod 定义了 BillingData 字段中 JSON 对象的结构
type BillingDataMod struct {
	Registrar      string `json:"registrar"`
	RegisteredDate string `json:"registeredDate"`
	EndDate        string `json:"endDate"`
	RenewalPrice   string `json:"renewalPrice"`
	AutoRenewal    string `json:"autoRenewal"`
	Notes          string `json:"notes"`
	// =======================================================
	// vvvvvvvvvvv 在这里加回缺失的字段 vvvvvvvvvvv
	Cycle  string `json:"cycle"`  // 续费周期，例如 "年" 或 "月"
	Amount string `json:"amount"` // 续费金额 (虽然 cron 中没用，但保留以保持结构完整)
	// ^^^^^^^^^^^ 在这里加回缺失的字段 ^^^^^^^^^^^
	// =======================================================
}

// DomainUpdateRequest 用于更新域名的请求体
type DomainUpdateRequest struct {
	IsPublic    bool           `json:"is_public"`
	BillingData datatypes.JSON `json:"billing_data"`
}

type DomainAPIRequest struct {
	Domain string `json:"domain" binding:"required"`
}
