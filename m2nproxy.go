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
	cert, err := tls.LoadX509KeyPair("cert.pem", "key.pem")
	if err != nil {
		log.Fatalf("Certificate error: %v", err)
	}

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
		go handleConnection(conn, &tls.Config{
			Certificates:       []tls.Certificate{cert},
			InsecureSkipVerify: true,
			MinVersion:         tls.VersionTLS12,
		})
	}
}

func handleConnection(conn net.Conn, tlsConfig *tls.Config) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Minute))

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	writer.WriteString("220 localhost ESMTP mail2news proxy\r\n")
	writer.Flush()

	var (
		message  strings.Builder
		usingTLS bool
	)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}

		line = strings.TrimRight(line, "\r\n")
		cmd := strings.ToUpper(strings.Fields(line)[0])

		switch cmd {
		case "EHLO", "HELO":
			writer.WriteString("250-localhost\r\n250-STARTTLS\r\n250-PIPELINING\r\n250 8BITMIME\r\n")
			writer.Flush()
		case "STARTTLS":
			if usingTLS {
				writer.WriteString("503 Already in TLS\r\n")
				writer.Flush()
			} else {
				writer.WriteString("220 Ready\r\n")
				writer.Flush()
				tlsConn := tls.Server(conn, tlsConfig)
				if err := tlsConn.Handshake(); err != nil {
					return
				}
				conn = tlsConn
				reader = bufio.NewReader(conn)
				writer = bufio.NewWriter(conn)
				usingTLS = true
			}
		case "MAIL":
			writer.WriteString("250 OK\r\n")
			writer.Flush()
		case "RCPT":
			writer.WriteString("250 OK\r\n")
			writer.Flush()
		case "DATA":
			writer.WriteString("354 End data with <CR><LF>.<CR><LF>\r\n")
			writer.Flush()

			message.Reset()
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimSpace(line) == "." {
					break
				}
				message.WriteString(line)
			}

			if err := forwardMessage(message.String()); err != nil {
				writer.WriteString("554 Forward error\r\n")
			} else {
				writer.WriteString("250 Message accepted\r\n")
			}
			writer.Flush()
		case "QUIT":
			writer.WriteString("221 Bye\r\n")
			writer.Flush()
			return
		default:
			writer.WriteString("502 Unsupported command\r\n")
			writer.Flush()
		}
	}
}

func forwardMessage(msg string) error {
	dialer, err := proxy.SOCKS5("tcp", torProxyAddr, nil, proxy.Direct)
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

	if err := client.Rcpt("mail2news@dizum.com"); err != nil {
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