// Package smtpd is the ingest side: a small SMTP server that accepts every
// message it is handed and stores it as a receipt. Routing (which addresses
// reach this app at all) is the upstream mail router's job.
package smtpd

import (
	"io"
	"log"
	"time"

	"github.com/baileybutler/shoebox/internal/ingest"
	"github.com/baileybutler/shoebox/internal/store"
	"github.com/emersion/go-smtp"
)

func ListenAndServe(addr, domain string, st *store.Store, loc *time.Location) error {
	s := smtp.NewServer(&backend{st: st, loc: loc})
	s.Addr = addr
	s.Domain = domain
	s.ReadTimeout = 60 * time.Second
	s.WriteTimeout = 30 * time.Second
	s.MaxMessageBytes = 30 << 20
	s.MaxRecipients = 20
	return s.ListenAndServe()
}

type backend struct {
	st  *store.Store
	loc *time.Location
}

func (b *backend) NewSession(_ *smtp.Conn) (smtp.Session, error) {
	return &session{st: b.st, loc: b.loc}, nil
}

type session struct {
	st   *store.Store
	loc  *time.Location
	rcpt string
}

func (s *session) Mail(from string, _ *smtp.MailOptions) error { return nil }

func (s *session) Rcpt(to string, _ *smtp.RcptOptions) error {
	if s.rcpt == "" {
		s.rcpt = to
	}
	return nil
}

func (s *session) Data(r io.Reader) error {
	raw, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	rec, err := s.st.Save(ingest.FromEmail(raw, s.rcpt, time.Now(), s.loc))
	if err != nil {
		log.Printf("smtp: store failed: %v", err)
		return &smtp.SMTPError{Code: 451, EnhancedCode: smtp.EnhancedCode{4, 3, 0}, Message: "storage failure"}
	}
	log.Printf("smtp: stored %s from=%s subject=%q files=%d", rec.ID, rec.From, rec.Subject, rec.FileCount())
	return nil
}

func (s *session) Reset()        { s.rcpt = "" }
func (s *session) Logout() error { return nil }
