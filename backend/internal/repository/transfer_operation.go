package repository

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"lng-boiloff-gas-balance/backend/internal/model"
	"lng-boiloff-gas-balance/backend/pkg/api"
)

type TransferFilter struct {
	TankID        uint
	OperationType string
	Status        string
	From          *time.Time
	To            *time.Time
	Page          int
	PageSize      int
}

type TransferRepository struct {
	db *gorm.DB
}

func NewTransferRepository(db *gorm.DB) *TransferRepository { return &TransferRepository{db: db} }

func (r *TransferRepository) List(ctx context.Context, filter TransferFilter) ([]model.TransferOperation, int64, error) {
	filter.Page, filter.PageSize = normalizePage(filter.Page, filter.PageSize)
	query := r.db.WithContext(ctx).Model(&model.TransferOperation{})
	if filter.TankID > 0 {
		query = query.Where("tank_id = ?", filter.TankID)
	}
	if filter.OperationType != "" {
		query = query.Where("operation_type = ?", filter.OperationType)
	}
	if filter.Status != "" {
		query = query.Where("operation_status = ?", filter.Status)
	}
	if filter.From != nil {
		query = query.Where("end_at >= ?", filter.From.UTC())
	}
	if filter.To != nil {
		query = query.Where("start_at <= ?", filter.To.UTC())
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count transfer operations: %w", err)
	}
	var items []model.TransferOperation
	if err := query.Preload("Tank").Order("start_at DESC, id DESC").
		Offset((filter.Page - 1) * filter.PageSize).Limit(filter.PageSize).Find(&items).Error; err != nil {
		return nil, 0, fmt.Errorf("list transfer operations: %w", err)
	}
	return items, total, nil
}

func (r *TransferRepository) Get(ctx context.Context, id uint) (model.TransferOperation, error) {
	var item model.TransferOperation
	if err := r.db.WithContext(ctx).Preload("Tank").First(&item, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return model.TransferOperation{}, api.NewError(404, "TRANSFER_NOT_FOUND", "物理转移记录不存在")
		}
		return model.TransferOperation{}, fmt.Errorf("get transfer operation: %w", err)
	}
	return item, nil
}

func (r *TransferRepository) Create(ctx context.Context, item *model.TransferOperation, actor Actor) (err error) {
	tx := r.db.WithContext(ctx).Begin()
	defer func() {
		err = tx.Commit().Error
	}()
	var overlaps int64
	if err = tx.Model(&model.TransferOperation{}).
		Where("tank_id = ? AND operation_status <> ? AND start_at < ? AND end_at > ?", item.TankID, "cancelled", item.EndAt, item.StartAt).
		Count(&overlaps).Error; err != nil {
		err = fmt.Errorf("check transfer time overlap: %w", err)
		return err
	}
	if overlaps > 0 {
		err = api.NewError(409, "TRANSFER_TIME_OVERLAP", "该储罐已有时间重叠的未取消物理转移")
		return err
	}
	if err = tx.Create(item).Error; err != nil {
		err = fmt.Errorf("create transfer operation: %w", err)
		return err
	}
	audit := NewAudit(actor, "transfer_operation.created", "transfer_operation", item.ID, nil, item)
	if err = tx.Create(&audit).Error; err != nil {
		err = fmt.Errorf("audit transfer operation: %w", err)
		return err
	}
	return nil
}

func (r *TransferRepository) Transition(ctx context.Context, id, version uint, target, reason string, actor Actor) (updated model.TransferOperation, err error) {
	tx := r.db.WithContext(ctx).Begin()
	defer func() {
		err = tx.Commit().Error
	}()
	var before model.TransferOperation
	if err = tx.First(&before, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			err = api.NewError(404, "TRANSFER_NOT_FOUND", "物理转移记录不存在")
			return updated, err
		}
		err = fmt.Errorf("load transfer operation: %w", err)
		return updated, err
	}
	if before.Version != version {
		err = api.NewError(409, "TRANSFER_VERSION_CONFLICT", "物理转移记录版本已变化，请刷新后重试")
		return updated, err
	}
	allowed := before.OperationStatus == "draft" && (target == "confirmed" || target == "cancelled")
	allowed = allowed || (before.OperationStatus == "confirmed" && target == "cancelled")
	if !allowed {
		err = api.WithDetails(api.NewError(409, "INVALID_TRANSFER_TRANSITION", "当前转移状态不允许目标迁移"), map[string]any{
			"current": before.OperationStatus, "target": target,
		})
		return updated, err
	}
	result := tx.Model(&model.TransferOperation{}).
		Where("id = ? AND version = ? AND operation_status = ?", id, version, before.OperationStatus).
		Updates(map[string]any{"operation_status": target, "version": gorm.Expr("version + 1")})
	if result.Error != nil {
		err = fmt.Errorf("transition transfer operation: %w", result.Error)
		return updated, err
	}
	if result.RowsAffected != 1 {
		err = api.NewError(409, "TRANSFER_VERSION_CONFLICT", "物理转移记录被其他请求更新")
		return updated, err
	}
	if err = tx.First(&updated, id).Error; err != nil {
		err = fmt.Errorf("reload transfer operation: %w", err)
		return updated, err
	}
	audit := NewAudit(actor, "transfer_operation."+target, "transfer_operation", id, before, map[string]any{"record": updated, "reason": reason})
	if err = tx.Create(&audit).Error; err != nil {
		err = fmt.Errorf("audit transfer transition: %w", err)
		return updated, err
	}
	return updated, nil
}

func (r *TransferRepository) ConfirmedForPeriod(ctx context.Context, tankID uint, start, end time.Time) ([]model.TransferOperation, error) {
	var items []model.TransferOperation
	if err := r.db.WithContext(ctx).
		Where("tank_id = ? AND operation_status = ? AND start_at >= ? AND end_at <= ?", tankID, "confirmed", start.UTC(), end.UTC()).
		Order("start_at ASC, id ASC").Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list confirmed period transfers: %w", err)
	}
	return items, nil
}
