package vps

import (
	"fmt"
	"log"
	"strings"
)

// Deliberately exclude event output: it can contain passwords or infrastructure secrets.
func customerEvent(action, level string) bool {
	if level != "success" && level != "error" {
		return false
	}
	switch action {
	case "terraform-prepare", "terraform-apply", "start", "shutdown", "restart", "root-password-reset", "hostname-update", "settings-update", "tunnel-http-route-create", "tunnel-http-route-update", "tunnel-http-route-delete", "tunnel-http-route-retry":
		return true
	case "terraform-destroy", "tunnel-setup":
		return level == "error"
	}
	return false
}

func (s *Service) notifyVPSEvent(id int64, message string) {
	rec, err := s.Get(id)
	if err != nil {
		log.Printf("VPS notification lookup failed vps_id=%d", id)
		return
	}
	s.notifyVPSOwner(rec.CustomerAccountID, rec.ID, rec.Name, message)
}

func (s *Service) notifyVPSOwner(accountID, id int64, name, summary string) {
	if accountID <= 0 {
		return
	}
	email, _, err := s.ownerEmail(accountID)
	if err == nil && strings.TrimSpace(email) == "" {
		log.Printf("VPS notification skipped: no customer email vps_id=%d", id)
		return
	}
	if err == nil {
		if s.mailer == nil {
			err = fmt.Errorf("mailer unavailable")
		} else {
			err = s.mailer.Send(email, "ProxaCloud VPS update", fmt.Sprintf("VPS: %s (ID %d)\n\n%s\n\nSign in to your ProxaCloud dashboard for details. Passwords are never included in these notifications.", name, id, summary))
		}
	}
	level := "info"
	message := "Customer notification queued"
	if err != nil {
		level = "error"
		message = "Customer notification could not be queued"
		log.Printf("VPS notification enqueue failed vps_id=%d", id)
	}
	// Deletion removes vps_events; the durable outbox remains independent of the VPS.
	_, _ = s.db.Exec(`INSERT INTO vps_events(vps_id,action,level,message,output) SELECT id,'customer-email',$2,$3,'' FROM vps_instances WHERE id=$1`, id, level, message)
}
