package main

import (
	"bufio"
	"crypto/tls"
	"log"
	"net"
	"net/smtp"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

const (
	torProxyAddr    = "127.0.0.1:9050"
	smtpRelayAddr   = "smtp.dizum.com:2525"
	localSMTPServer = ":2525"
)

func main() {
	listener, err := net.Listen("tcp", localSMTPServer)
	if err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
	defer listener.Close()

	log.Printf("SMTP proxy running on %s", localSMTPServer)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}
		go handleConnection(conn)
	}
}

func handleConnection(conn net.Conn) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Minute))

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	writer.WriteString("220 localhost ESMTP mail2news proxy\r\n")
	writer.Flush()

	var message strings.Builder
	to := "mail2news@dizum.com"

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err.Error() != "EOF" {
				log.Printf("Read error: %v", err)
			}
			return
		}

		line = strings.TrimRight(line, "\r\n")
		if len(line) == 0 {
			continue
		}

		cmd := strings.ToUpper(strings.Fields(line)[0])
		switch cmd {
		case "EHLO", "HELO":
			writer.WriteString("250-localhost\r\n250-PIPELINING\r\n250 8BITMIME\r\n")
		case "MAIL":
			writer.WriteString("250 OK\r\n")
		case "RCPT":
			writer.WriteString("250 OK\r\n")
		case "DATA":
			writer.WriteString("354 End data with <CR><LF>.<CR><LF>\r\n")
			writer.Flush()

			message.Reset()
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					log.Printf("DATA read error: %v", err)
					return
				}
				if strings.TrimSpace(line) == "." {
					break
				}
				message.WriteString(line)
			}

			if err := forwardMessage(to, message.String()); err != nil {
				log.Printf("Forward error: %v", err)
				writer.WriteString("554 Forward error\r\n")
			} else {
				writer.WriteString("250 Message accepted\r\n")
			}
		case "QUIT":
			writer.WriteString("221 Bye\r\n")
			writer.Flush()
			return
		default:
			writer.WriteString("502 Unsupported command\r\n")
		}
		writer.Flush()
	}
}

func forwardMessage(to, msg string) error {
	dialer, err := proxy.SOCKS5("tcp", torProxyAddr, nil, &net.Dialer{
		Timeout:   2 * time.Minute,
		KeepAlive: 1 * time.Minute,
	})
	if err != nil {
		return err
	}

	conn, err := dialer.Dial("tcp", smtpRelayAddr)
	if err != nil {
		return err
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, smtpRelayAddr)
	if err != nil {
		return err
	}
	defer client.Quit()

	if err := client.StartTLS(&tls.Config{
		ServerName: "smtp.dizum.com",
		InsecureSkipVerify: true,
	}); err != nil {
		return err
	}

	if err := client.Mail(""); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}

	w, err := client.Data()
	if err != nil {
		return err
	}
	defer w.Close()

	if _, err := w.Write([]byte(msg)); err != nil {
		return err
	}

	return nil
}