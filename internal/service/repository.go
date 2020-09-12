package service

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/m-mizutani/deepalert"
	"github.com/m-mizutani/deepalert/internal/adaptor"
	"github.com/m-mizutani/deepalert/internal/errors"
	"github.com/m-mizutani/deepalert/internal/models"
)

type RepositoryService struct {
	repo adaptor.Repository
	ttl  time.Duration
}

func NewRepositoryService(repo adaptor.Repository, ttl int64) *RepositoryService {
	return &RepositoryService{
		repo: repo,
		ttl:  time.Duration(ttl) * time.Second,
	}
}

// -----------------------------------------------------------
// Control alertEntry to manage AlertID to ReportID mapping
//

func newReportID() deepalert.ReportID {
	return deepalert.ReportID(uuid.New().String())
}

func (x *RepositoryService) TakeReport(alert deepalert.Alert, now time.Time) (*deepalert.Report, error) {
	fixedKey := "Fixed"
	alertID := alert.AlertID()

	entry := models.AlertEntry{
		RecordBase: models.RecordBase{
			PKey:      "alertmap/" + alertID,
			SKey:      fixedKey,
			ExpiresAt: now.Add(x.ttl).Unix(),
			CreatedAt: now,
		},
		ReportID: newReportID(),
	}

	if err := x.repo.PutAlertEntry(&entry, now); err != nil {
		if x.repo.IsConditionalCheckErr(err) {
			existedEntry, err := x.repo.GetAlertEntry(entry.PKey, entry.SKey)
			if err != nil {
				return nil, errors.Wrap(err, "Fail to get cached reportID").With("AlertID", alertID)
			}

			return &deepalert.Report{
				ID:        existedEntry.ReportID,
				Status:    deepalert.StatusMore,
				CreatedAt: existedEntry.CreatedAt,
			}, nil
		}

		return nil, errors.Wrapf(err, "Fail to get cached reportID, AlertID=%s", alertID)
	}

	return &deepalert.Report{
		ID:        entry.ReportID,
		Status:    deepalert.StatusNew,
		CreatedAt: now,
	}, nil
}

// -----------------------------------------------------------
// Control alertCache to manage published alert data
//

func toAlertCacheKey(reportID deepalert.ReportID) (string, string) {
	return fmt.Sprintf("alert/%s", reportID), "cache/" + uuid.New().String()
}

func (x *RepositoryService) SaveAlertCache(reportID deepalert.ReportID, alert deepalert.Alert, now time.Time) error {
	raw, err := json.Marshal(alert)
	if err != nil {
		return errors.Wrapf(err, "Fail to marshal alert: %v", alert)
	}

	pk, sk := toAlertCacheKey(reportID)
	cache := &models.AlertCache{
		PKey:      pk,
		SKey:      sk,
		AlertData: raw,
		ExpiresAt: now.UTC().Add(x.ttl).Unix(),
	}

	if err := x.repo.PutAlertCache(cache); err != nil {
		return err
	}

	return nil
}

func (x *RepositoryService) FetchAlertCache(reportID deepalert.ReportID) ([]deepalert.Alert, error) {
	pk, _ := toAlertCacheKey(reportID)
	var alerts []deepalert.Alert

	caches, err := x.repo.GetAlertCaches(pk)
	if err != nil {
		return nil, errors.Wrap(err, "GetAlertCaches").With("reportID", reportID)
	}

	for _, cache := range caches {
		var alert deepalert.Alert
		if err := json.Unmarshal(cache.AlertData, &alert); err != nil {
			return nil, errors.Wrap(err, "Fail to unmarshal alert").With("data", string(cache.AlertData))
		}
		alerts = append(alerts, alert)
	}

	return alerts, nil
}

// -----------------------------------------------------------
// Control reportRecord to manage report contents by inspector
//

func toReportSectionRecord(reportID deepalert.ReportID, section *deepalert.ReportSection) (string, string) {
	pk := fmt.Sprintf("content/%s", reportID)
	sk := ""
	if section != nil {
		sk = fmt.Sprintf("%s/%s", section.Attribute.Hash(), uuid.New().String())
	}
	return pk, sk
}

func (x *RepositoryService) SaveReportSection(section deepalert.ReportSection, now time.Time) error {
	raw, err := json.Marshal(section)
	if err != nil {
		return errors.Wrapf(err, "Fail to marshal ReportSection: %v", section)
	}

	pk, sk := toReportSectionRecord(section.ReportID, &section)
	record := &models.ReportSectionRecord{
		RecordBase: models.RecordBase{
			PKey:      pk,
			SKey:      sk,
			ExpiresAt: now.UTC().Add(x.ttl).Unix(),
		},
		Data: raw,
	}

	if err := x.repo.PutReportSectionRecord(record); err != nil {
		return errors.Wrap(err, "Fail to put report record")
	}

	return nil
}

func (x *RepositoryService) FetchReportSection(reportID deepalert.ReportID) ([]deepalert.ReportSection, error) {
	pk, _ := toReportSectionRecord(reportID, nil)

	records, err := x.repo.GetReportSection(pk)
	if err != nil {
		return nil, err
	}

	var sections []deepalert.ReportSection
	for _, record := range records {
		var section deepalert.ReportSection
		if err := json.Unmarshal(record.Data, &section); err != nil {
			return nil, errors.Wrapf(err, "Fail to unmarshal report content: %v %s", record, string(record.Data))
		}

		sections = append(sections, section)
	}

	return sections, nil
}

// -----------------------------------------------------------
// Control attribute cache to prevent duplicated invocation of Inspector with same attribute
//

func toAttributeCacheKey(reportID deepalert.ReportID) string {
	return fmt.Sprintf("attribute/%s", reportID)
}

// PutAttributeCache puts attributeCache to DB and returns true. If the attribute alrady exists,
// it returns false.
func (x *RepositoryService) PutAttributeCache(reportID deepalert.ReportID, attr deepalert.Attribute, now time.Time) (bool, error) {
	var ts time.Time
	if attr.Timestamp != nil {
		ts = *attr.Timestamp
	} else {
		ts = now
	}

	cache := &models.AttributeCache{
		RecordBase: models.RecordBase{
			PKey:      toAttributeCacheKey(reportID),
			SKey:      attr.Hash(),
			ExpiresAt: now.Add(x.ttl).Unix(),
		},
		Timestamp:   ts,
		AttrKey:     attr.Key,
		AttrType:    string(attr.Type),
		AttrValue:   attr.Value,
		AttrContext: attr.Context,
	}

	if err := x.repo.PutAttributeCache(cache, now); err != nil {
		if x.repo.IsConditionalCheckErr(err) {
			// The attribute already exists
			return false, nil
		}

		return false, errors.Wrapf(err, "Fail to put attr cache reportID=%s, %v", reportID, attr)
	}

	return true, nil
}

// FetchAttributeCache retrieves all cached attribute from DB.
func (x *RepositoryService) FetchAttributeCache(reportID deepalert.ReportID) ([]deepalert.Attribute, error) {
	pk := toAttributeCacheKey(reportID)

	caches, err := x.repo.GetAttributeCaches(pk)
	if err != nil {
		return nil, errors.Wrapf(err, "Fail to retrieve attributeCache: %s", reportID)
	}

	var attrs []deepalert.Attribute
	for _, cache := range caches {
		attr := deepalert.Attribute{
			Type:      deepalert.AttrType(cache.AttrType),
			Key:       cache.AttrKey,
			Value:     cache.AttrValue,
			Context:   cache.AttrContext,
			Timestamp: &cache.Timestamp,
		}

		attrs = append(attrs, attr)
	}

	return attrs, nil
}