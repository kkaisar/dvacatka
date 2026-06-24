package handlers

import (
	"fmt"
	"log"
	"net/smtp"

	"dvacatka/config"
)

// sendResetEmail отправляет письмо со ссылкой сброса пароля.
// Если SMTP не настроен (нет SMTP_USER), ссылка пишется в лог — удобно при разработке.
func sendResetEmail(cfg *config.Config, toEmail, resetLink string) error {
	if cfg.SMTPUser == "" || cfg.SMTPPass == "" {
		log.Printf("mailer: SMTP не настроен — ссылка сброса для %s: %s", toEmail, resetLink)
		return nil
	}

	subject := "Dvacatka — восстановление пароля"
	body := fmt.Sprintf("Здравствуйте!\r\n\r\n"+
		"Для сброса пароля перейдите по ссылке (действует 1 час):\r\n%s\r\n\r\n"+
		"Если вы не запрашивали сброс — просто проигнорируйте это письмо.\r\n", resetLink)

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\n"+
		"Content-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		cfg.SMTPUser, toEmail, subject, body)

	addr := cfg.SMTPHost + ":" + cfg.SMTPPort
	auth := smtp.PlainAuth("", cfg.SMTPUser, cfg.SMTPPass, cfg.SMTPHost)
	if err := smtp.SendMail(addr, auth, cfg.SMTPUser, []string{toEmail}, []byte(msg)); err != nil {
		log.Printf("mailer: ошибка отправки на %s: %v", toEmail, err)
		return err
	}
	log.Printf("mailer: письмо сброса отправлено на %s", toEmail)
	return nil
}
