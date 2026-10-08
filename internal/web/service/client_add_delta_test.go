package service

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func installClientAddDeltaAudit(t *testing.T) {
	t.Helper()
	db := database.GetDB()
	statements := []string{
		`CREATE TABLE client_add_delta_audit (event TEXT NOT NULL)`,
		`CREATE TRIGGER audit_client_add_link_insert AFTER INSERT ON client_inbounds BEGIN INSERT INTO client_add_delta_audit(event) VALUES ('link_insert'); END`,
		`CREATE TRIGGER audit_client_add_link_delete AFTER DELETE ON client_inbounds BEGIN INSERT INTO client_add_delta_audit(event) VALUES ('link_delete'); END`,
		`CREATE TRIGGER audit_client_add_mapping_insert AFTER INSERT ON client_inbound_traffics BEGIN INSERT INTO client_add_delta_audit(event) VALUES ('mapping_insert'); END`,
		`CREATE TRIGGER audit_client_add_mapping_update AFTER UPDATE ON client_inbound_traffics BEGIN INSERT INTO client_add_delta_audit(event) VALUES ('mapping_update'); END`,
		`CREATE TRIGGER audit_client_add_record_insert AFTER INSERT ON clients BEGIN INSERT INTO client_add_delta_audit(event) VALUES ('record_insert'); END`,
		`CREATE TRIGGER audit_client_add_record_update AFTER UPDATE ON clients BEGIN INSERT INTO client_add_delta_audit(event) VALUES ('record_update'); END`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("install delta audit: %v", err)
		}
	}
}

func clientAddDeltaAuditCount(t *testing.T, event string) int64 {
	t.Helper()
	var count int64
	if err := database.GetDB().Table("client_add_delta_audit").Where("event = ?", event).Count(&count).Error; err != nil {
		t.Fatalf("count delta audit event %q: %v", event, err)
	}
	return count
}

func seedClientInboundTrafficMappings(t *testing.T, inboundID int) []model.ClientInboundTraffic {
	t.Helper()
	db := database.GetDB()
	var clients []struct {
		ID    int    `gorm:"column:id"`
		Email string `gorm:"column:email"`
	}
	if err := db.Table("clients").
		Select("clients.id AS id, clients.email AS email").
		Joins("JOIN client_inbounds ON client_inbounds.client_id = clients.id").
		Where("client_inbounds.inbound_id = ?", inboundID).
		Order("clients.id ASC").
		Scan(&clients).Error; err != nil {
		t.Fatalf("load clients for mapping seed: %v", err)
	}

	now := time.Now().UnixMilli()
	mappings := make([]model.ClientInboundTraffic, len(clients))
	for i := range clients {
		mappings[i] = model.ClientInboundTraffic{
			ClientID:     clients[i].ID,
			InboundID:    inboundID,
			Email:        clients[i].Email,
			StatEmail:    clientInboundStatEmail(clients[i].Email, inboundID),
			ActualUp:     int64(1000 + i),
			ActualDown:   int64(2000 + i),
			BillableUp:   int64(3000 + i),
			BillableDown: int64(4000 + i),
			LastOnline:   int64(5000 + i),
			CreatedAt:    now,
			UpdatedAt:    now,
		}
	}
	if len(mappings) > 0 {
		if err := db.CreateInBatches(&mappings, 200).Error; err != nil {
			t.Fatalf("seed client inbound traffic mappings: %v", err)
		}
	}
	return mappings
}

func TestAddInboundClientDeltaPreservesThousandsOfExistingRows(t *testing.T) {
	setupBulkDB(t)
	nodeID, _ := setupNodeRuntime(t)

	const existingCount = 3000
	existing := makeNodeClients(existingCount)
	for i := range existing {
		existing[i].SubID = fmt.Sprintf("delta-existing-%05d", i)
	}
	inbound := nodeInbound(t, nodeID, 47001, existing)
	mappings := seedClientInboundTrafficMappings(t, inbound.Id)
	if len(mappings) != existingCount {
		t.Fatalf("seeded mappings = %d, want %d", len(mappings), existingCount)
	}
	preserved := mappings[existingCount/2]

	installClientAddDeltaAudit(t)
	newClient := model.Client{
		ID:     uuid.NewString(),
		Email:  "delta-new@x",
		SubID:  "delta-new-sub-id",
		Enable: true,
	}
	if _, err := (&ClientService{}).AddInboundClient(
		&InboundService{},
		&model.Inbound{Id: inbound.Id, Settings: clientsSettings(t, []model.Client{newClient})},
	); err != nil {
		t.Fatalf("AddInboundClient: %v", err)
	}

	if got := clientAddDeltaAuditCount(t, "link_delete"); got != 0 {
		t.Fatalf("deleted existing client_inbounds rows = %d, want 0", got)
	}
	if got := clientAddDeltaAuditCount(t, "link_insert"); got != 1 {
		t.Fatalf("inserted client_inbounds rows = %d, want 1", got)
	}
	if got := clientAddDeltaAuditCount(t, "mapping_update"); got != 0 {
		t.Fatalf("updated existing client_inbound_traffics rows = %d, want 0", got)
	}
	if got := clientAddDeltaAuditCount(t, "mapping_insert"); got != 1 {
		t.Fatalf("inserted client_inbound_traffics rows = %d, want 1", got)
	}
	if got := clientAddDeltaAuditCount(t, "record_update"); got != 0 {
		t.Fatalf("updated existing clients rows = %d, want 0", got)
	}
	if got := clientAddDeltaAuditCount(t, "record_insert"); got != 1 {
		t.Fatalf("inserted clients rows = %d, want 1", got)
	}

	var linkCount int64
	if err := database.GetDB().Model(&model.ClientInbound{}).
		Where("inbound_id = ?", inbound.Id).Count(&linkCount).Error; err != nil {
		t.Fatalf("count links: %v", err)
	}
	if linkCount != existingCount+1 {
		t.Fatalf("links = %d, want %d", linkCount, existingCount+1)
	}

	var mappingCount int64
	if err := database.GetDB().Model(&model.ClientInboundTraffic{}).
		Where("inbound_id = ?", inbound.Id).Count(&mappingCount).Error; err != nil {
		t.Fatalf("count mappings: %v", err)
	}
	if mappingCount != existingCount+1 {
		t.Fatalf("mappings = %d, want %d", mappingCount, existingCount+1)
	}

	var gotPreserved model.ClientInboundTraffic
	if err := database.GetDB().First(&gotPreserved, preserved.Id).Error; err != nil {
		t.Fatalf("load preserved mapping: %v", err)
	}
	if gotPreserved.ActualUp != preserved.ActualUp ||
		gotPreserved.ActualDown != preserved.ActualDown ||
		gotPreserved.BillableUp != preserved.BillableUp ||
		gotPreserved.BillableDown != preserved.BillableDown ||
		gotPreserved.LastOnline != preserved.LastOnline {
		t.Fatalf("existing traffic mapping changed: got %#v, want %#v", gotPreserved, preserved)
	}

	var newMapping model.ClientInboundTraffic
	if err := database.GetDB().Where("inbound_id = ? AND email = ?", inbound.Id, newClient.Email).
		First(&newMapping).Error; err != nil {
		t.Fatalf("load new mapping: %v", err)
	}
	if newMapping.StatEmail != clientInboundStatEmail(newClient.Email, inbound.Id) {
		t.Fatalf("new mapping stat_email was not created for the target inbound")
	}

	stored, err := (&InboundService{}).GetInbound(inbound.Id)
	if err != nil {
		t.Fatalf("GetInbound: %v", err)
	}
	storedClients, err := (&InboundService{}).GetClients(stored)
	if err != nil {
		t.Fatalf("GetClients: %v", err)
	}
	if len(storedClients) != existingCount+1 {
		t.Fatalf("settings clients = %d, want %d", len(storedClients), existingCount+1)
	}
}

func TestAddInboundClientDeltaAttachesExistingIdentityAcrossProtocolsAndRetries(t *testing.T) {
	setupBulkDB(t)
	nodeID, _ := setupNodeRuntime(t)
	db := database.GetDB()

	const (
		email  = "delta-shared@x"
		subID  = "delta-shared-sub"
		vision = "xtls-rprx-vision"
	)
	sourceClient := model.Client{
		ID:     uuid.NewString(),
		Email:  email,
		SubID:  subID,
		Enable: true,
		Flow:   vision,
	}
	source := nodeInbound(t, nodeID, 47002, []model.Client{sourceClient})
	if err := db.Model(&model.Inbound{}).Where("id = ?", source.Id).
		Update("stream_settings", `{"network":"tcp","security":"reality"}`).Error; err != nil {
		t.Fatalf("set source stream settings: %v", err)
	}

	reality := nodeInbound(t, nodeID, 47003, nil)
	if err := db.Model(&model.Inbound{}).Where("id = ?", reality.Id).
		Update("stream_settings", `{"network":"tcp","security":"reality"}`).Error; err != nil {
		t.Fatalf("set target stream settings: %v", err)
	}
	trojan := &model.Inbound{
		UserId:   1,
		NodeID:   &nodeID,
		Tag:      "delta-trojan",
		Enable:   true,
		Port:     47004,
		Protocol: model.Trojan,
		Settings: `{"clients":[]}`,
	}
	if err := db.Create(trojan).Error; err != nil {
		t.Fatalf("create trojan inbound: %v", err)
	}

	var before model.ClientRecord
	if err := db.Where("email = ?", email).First(&before).Error; err != nil {
		t.Fatalf("load source record: %v", err)
	}
	if err := db.Model(&model.ClientRecord{}).Where("id = ?", before.Id).Updates(map[string]any{
		"owner_admin_id":      41,
		"created_by_admin_id": 42,
	}).Error; err != nil {
		t.Fatalf("seed ownership fields: %v", err)
	}
	if err := db.Where("id = ?", before.Id).First(&before).Error; err != nil {
		t.Fatalf("reload source record: %v", err)
	}

	payload := &ClientCreatePayload{
		Client: model.Client{
			Email:  email,
			SubID:  subID,
			Enable: true,
			Flow:   vision,
		},
		InboundIds: []int{reality.Id, trojan.Id},
	}
	svc := &ClientService{}
	inboundSvc := &InboundService{}
	for attempt := 1; attempt <= 2; attempt++ {
		if _, err := svc.Create(inboundSvc, payload); err != nil {
			t.Fatalf("Create attempt %d: %v", attempt, err)
		}
	}

	var after model.ClientRecord
	if err := db.Where("email = ?", email).First(&after).Error; err != nil {
		t.Fatalf("load attached record: %v", err)
	}
	if after.Id != before.Id || after.ClientGuid != before.ClientGuid || after.UUID != before.UUID {
		t.Fatalf("logical identity or VLESS credential changed: before=%#v after=%#v", before, after)
	}
	if after.Password == "" {
		t.Fatal("Trojan credential was not added to the existing logical client")
	}
	if after.OwnerAdminId != 41 || after.CreatedByAdminId != 42 || after.CreatedAt != before.CreatedAt {
		t.Fatalf("ownership/creation fields changed: before=%#v after=%#v", before, after)
	}

	var links []model.ClientInbound
	if err := db.Where("client_id = ?", before.Id).Order("inbound_id ASC").Find(&links).Error; err != nil {
		t.Fatalf("load links: %v", err)
	}
	if len(links) != 3 {
		t.Fatalf("links after retry = %d, want 3", len(links))
	}
	flowByInbound := make(map[int]string, len(links))
	for i := range links {
		flowByInbound[links[i].InboundId] = links[i].FlowOverride
	}
	if flowByInbound[source.Id] != vision || flowByInbound[reality.Id] != vision || flowByInbound[trojan.Id] != "" {
		t.Fatalf("flow overrides = %#v", flowByInbound)
	}

	for _, inboundID := range []int{reality.Id, trojan.Id} {
		stored, err := inboundSvc.GetInbound(inboundID)
		if err != nil {
			t.Fatalf("GetInbound(%d): %v", inboundID, err)
		}
		clients, err := inboundSvc.GetClients(stored)
		if err != nil {
			t.Fatalf("GetClients(%d): %v", inboundID, err)
		}
		if len(clients) != 1 || clients[0].Email != email {
			t.Fatalf("inbound %d settings after retry = %#v", inboundID, clients)
		}
	}

	var mappingCount int64
	if err := db.Model(&model.ClientInboundTraffic{}).Where("client_id = ?", before.Id).Count(&mappingCount).Error; err != nil {
		t.Fatalf("count mappings: %v", err)
	}
	if mappingCount != 2 {
		t.Fatalf("new inbound mappings after retry = %d, want 2", mappingCount)
	}
}

func TestAddInboundClientDeltaFallsBackForMissingNormalizedLink(t *testing.T) {
	setupBulkDB(t)
	nodeID, _ := setupNodeRuntime(t)
	db := database.GetDB()

	existing := []model.Client{
		{ID: uuid.NewString(), Email: "fallback-a@x", SubID: "fallback-a-sub", Enable: true},
		{ID: uuid.NewString(), Email: "fallback-b@x", SubID: "fallback-b-sub", Enable: true},
	}
	inbound := nodeInbound(t, nodeID, 47005, existing)
	mappings := seedClientInboundTrafficMappings(t, inbound.Id)
	mappings[0].ActualUp = 987654
	if err := db.Model(&model.ClientInboundTraffic{}).Where("id = ?", mappings[0].Id).
		Update("actual_up", mappings[0].ActualUp).Error; err != nil {
		t.Fatalf("seed accumulated traffic: %v", err)
	}

	var missingRecord model.ClientRecord
	if err := db.Where("email = ?", existing[1].Email).First(&missingRecord).Error; err != nil {
		t.Fatalf("load record to unlink: %v", err)
	}
	if err := db.Where("client_id = ? AND inbound_id = ?", missingRecord.Id, inbound.Id).
		Delete(&model.ClientInbound{}).Error; err != nil {
		t.Fatalf("remove normalized link: %v", err)
	}

	installClientAddDeltaAudit(t)
	newClient := model.Client{ID: uuid.NewString(), Email: "fallback-new@x", SubID: "fallback-new-sub", Enable: true}
	if _, err := (&ClientService{}).AddInboundClient(
		&InboundService{},
		&model.Inbound{Id: inbound.Id, Settings: clientsSettings(t, []model.Client{newClient})},
	); err != nil {
		t.Fatalf("AddInboundClient: %v", err)
	}

	if got := clientAddDeltaAuditCount(t, "link_delete"); got != 1 {
		t.Fatalf("fallback deleted links = %d, want 1", got)
	}
	if got := clientAddDeltaAuditCount(t, "link_insert"); got != 3 {
		t.Fatalf("fallback recreated links = %d, want 3", got)
	}
	var linkCount int64
	if err := db.Model(&model.ClientInbound{}).Where("inbound_id = ?", inbound.Id).Count(&linkCount).Error; err != nil {
		t.Fatalf("count repaired links: %v", err)
	}
	if linkCount != 3 {
		t.Fatalf("repaired links = %d, want 3", linkCount)
	}

	var preserved model.ClientInboundTraffic
	if err := db.First(&preserved, mappings[0].Id).Error; err != nil {
		t.Fatalf("load preserved mapping: %v", err)
	}
	if preserved.ActualUp != 987654 {
		t.Fatalf("fallback lost accumulated traffic: got %d", preserved.ActualUp)
	}
}

func TestAddInboundClientDeltaTransactionFailureRollsBack(t *testing.T) {
	setupBulkDB(t)
	nodeID, fake := setupNodeRuntime(t)
	db := database.GetDB()

	existing := model.Client{ID: uuid.NewString(), Email: "rollback-existing@x", SubID: "rollback-existing-sub", Enable: true}
	inbound := nodeInbound(t, nodeID, 47006, []model.Client{existing})
	originalSettings := inbound.Settings

	trigger := fmt.Sprintf(`
		CREATE TRIGGER fail_client_add_delta_link
		BEFORE INSERT ON client_inbounds
		WHEN NEW.inbound_id = %d
		BEGIN
			SELECT RAISE(ABORT, 'forced client add delta failure');
		END`, inbound.Id)
	if err := db.Exec(trigger).Error; err != nil {
		t.Fatalf("install failure trigger: %v", err)
	}

	newClient := model.Client{ID: uuid.NewString(), Email: "rollback-new@x", SubID: "rollback-new-sub", Enable: true}
	if _, err := (&ClientService{}).AddInboundClient(
		&InboundService{},
		&model.Inbound{Id: inbound.Id, Settings: clientsSettings(t, []model.Client{newClient})},
	); err == nil {
		t.Fatal("AddInboundClient succeeded despite forced link failure")
	}

	stored, err := (&InboundService{}).GetInbound(inbound.Id)
	if err != nil {
		t.Fatalf("GetInbound: %v", err)
	}
	if stored.Settings != originalSettings {
		t.Fatal("inbound settings were not rolled back")
	}

	var recordCount int64
	if err := db.Model(&model.ClientRecord{}).Where("email = ?", newClient.Email).Count(&recordCount).Error; err != nil {
		t.Fatalf("count rolled-back client record: %v", err)
	}
	if recordCount != 0 {
		t.Fatalf("new client records after rollback = %d, want 0", recordCount)
	}
	var trafficCount int64
	if err := db.Table("client_traffics").Where("email = ?", newClient.Email).Count(&trafficCount).Error; err != nil {
		t.Fatalf("count rolled-back traffic row: %v", err)
	}
	if trafficCount != 0 {
		t.Fatalf("new client traffic rows after rollback = %d, want 0", trafficCount)
	}
	var mappingCount int64
	if err := db.Model(&model.ClientInboundTraffic{}).Where("email = ?", newClient.Email).Count(&mappingCount).Error; err != nil {
		t.Fatalf("count rolled-back mapping: %v", err)
	}
	if mappingCount != 0 {
		t.Fatalf("new mappings after rollback = %d, want 0", mappingCount)
	}
	var linkCount int64
	if err := db.Model(&model.ClientInbound{}).Where("inbound_id = ?", inbound.Id).Count(&linkCount).Error; err != nil {
		t.Fatalf("count links after rollback: %v", err)
	}
	if linkCount != 1 {
		t.Fatalf("links after rollback = %d, want 1", linkCount)
	}
	if got := fake.addClient.Load(); got != 0 {
		t.Fatalf("runtime AddClient calls after transaction failure = %d, want 0", got)
	}
}
