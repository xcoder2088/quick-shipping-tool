// Copyright (c) 2025-2026 Francois "Brad" Bradette. All rights reserved.
// Proprietary and confidential. Not open source. See LICENSE.
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/mail"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	maxUploadSize = 50 << 20 // 50 MB
	maxPhotos     = 4

	mailerSendURL = "https://api.mailersend.com/v1/email"
)

// ==================================================
// API RESPONSE TO OUR PWA
// ==================================================

type UploadResponse struct {
	Success      bool     `json:"success"`
	Message      string   `json:"message"`
	Customer     string   `json:"customer,omitempty"`
	Shipment     string   `json:"shipment,omitempty"`
	Destination  string   `json:"destination,omitempty"`
	Destinations []string `json:"destinations,omitempty"`
	UserEmail    string   `json:"user_email,omitempty"`
	Photos       int      `json:"photos,omitempty"`
	MessageID    string   `json:"message_id,omitempty"`
}

// ==================================================
// MAILERSEND STRUCTURES
// ==================================================

type MailerSendAddress struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

type MailerSendAttachment struct {
	Content     string `json:"content"`
	Filename    string `json:"filename"`
	Disposition string `json:"disposition,omitempty"`
}

type MailerSendRequest struct {
	From        MailerSendAddress      `json:"from"`
	To          []MailerSendAddress    `json:"to"`
	Cc          []MailerSendAddress    `json:"cc,omitempty"`
	ReplyTo     MailerSendAddress      `json:"reply_to"`
	Subject     string                 `json:"subject"`
	Text        string                 `json:"text"`
	HTML        string                 `json:"html,omitempty"`
	Attachments []MailerSendAttachment `json:"attachments,omitempty"`
}

// ==================================================
// MAIN
// ==================================================

func main() {

	// ==================================================
	// STATIC FILES
	// ==================================================

	fs := http.FileServer(
		http.Dir("./static"),
	)

	http.Handle(
		"/static/",
		http.StripPrefix(
			"/static/",
			fs,
		),
	)

	// ==================================================
	// ROUTES
	// ==================================================

	http.HandleFunc(
		"/",
		homeHandler,
	)

	http.HandleFunc(
		"/manifest.webmanifest",
		manifestHandler,
	)

	http.HandleFunc(
		"/service-worker.js",
		serviceWorkerHandler,
	)

	http.HandleFunc(
		"/api/upload",
		rateLimit("upload", 10, time.Minute, uploadHandler),
	)

	http.HandleFunc(
		"/privacy",
		privacyHandler,
	)

	http.HandleFunc(
		"/support",
		supportHandler,
	)

	http.HandleFunc(
		"/terms",
		termsHandler,
	)

	http.HandleFunc(
		"/.well-known/assetlinks.json",
		assetLinksHandler,
	)

	// ==================================================
	// CHECK MAILERSEND CONFIGURATION
	// ==================================================

	apiKey := strings.TrimSpace(
		os.Getenv("MAILERSEND_API_KEY"),
	)

	fromEmail := strings.TrimSpace(
		os.Getenv("MAILERSEND_FROM"),
	)

	fmt.Println("=================================")
	fmt.Println("  QuickProof")
	fmt.Println("  http://localhost:8081")

	if apiKey != "" && fromEmail != "" {
		fmt.Println("  MailerSend: READY")
	} else {
		fmt.Println("  MailerSend: NOT CONFIGURED")
	}

	fmt.Println("=================================")

	// ==================================================
	// SERVER
	// ==================================================

	log.Fatal(
		http.ListenAndServe(
			":8081",
			securityHeaders(http.DefaultServeMux),
		),
	)
}

// ==================================================
// PWA FILES
// ==================================================

func manifestHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.URL.Path != "/manifest.webmanifest" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/manifest+json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, "./static/manifest.webmanifest")
}

func serviceWorkerHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.URL.Path != "/service-worker.js" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Service-Worker-Allowed", "/")
	http.ServeFile(w, r, "./static/service-worker.js")
}

// ==================================================
// HOME
// ==================================================

func homeHandler(
	w http.ResponseWriter,
	r *http.Request,
) {

	if r.URL.Path != "/" {

		http.NotFound(
			w,
			r,
		)

		return
	}

	http.ServeFile(
		w,
		r,
		"./templates/index.html",
	)
}

func privacyHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.URL.Path != "/privacy" {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, "./templates/privacy.html")
}

func supportHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.URL.Path != "/support" {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, "./templates/support.html")
}

func termsHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.URL.Path != "/terms" {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, "./templates/terms.html")
}

// ==================================================
// UPLOAD
// ==================================================

func uploadHandler(
	w http.ResponseWriter,
	r *http.Request,
) {

	// ==================================================
	// POST ONLY
	// ==================================================

	if r.Method != http.MethodPost {

		writeJSON(
			w,
			http.StatusMethodNotAllowed,
			UploadResponse{
				Success: false,
				Message: "Method not allowed",
			},
		)

		return
	}

	// ==================================================
	// MAILERSEND CONFIGURATION
	// ==================================================

	apiKey := strings.TrimSpace(
		os.Getenv("MAILERSEND_API_KEY"),
	)

	fromEmail := strings.TrimSpace(
		os.Getenv("MAILERSEND_FROM"),
	)

	if apiKey == "" {

		log.Println(
			"MAILERSEND_API_KEY is not configured",
		)

		writeJSON(
			w,
			http.StatusInternalServerError,
			UploadResponse{
				Success: false,
				Message: "Email service is not configured",
			},
		)

		return
	}

	if fromEmail == "" {

		log.Println(
			"MAILERSEND_FROM is not configured",
		)

		writeJSON(
			w,
			http.StatusInternalServerError,
			UploadResponse{
				Success: false,
				Message: "Sender email is not configured",
			},
		)

		return
	}

	if !validEmail(fromEmail) {

		log.Println(
			"MAILERSEND_FROM contains an invalid email address",
		)

		writeJSON(
			w,
			http.StatusInternalServerError,
			UploadResponse{
				Success: false,
				Message: "Sender email configuration is invalid",
			},
		)

		return
	}

	// ==================================================
	// UPLOAD SIZE LIMIT
	// ==================================================

	r.Body = http.MaxBytesReader(
		w,
		r.Body,
		maxUploadSize,
	)

	if err := r.ParseMultipartForm(
		maxUploadSize,
	); err != nil {

		writeJSON(
			w,
			http.StatusBadRequest,
			UploadResponse{
				Success: false,
				Message: "Unable to process upload. The files may be too large.",
			},
		)

		return
	}

	// ==================================================
	// CUSTOMER
	// ==================================================

	customer := strings.TrimSpace(
		r.FormValue("customer"),
	)

	// ==================================================
	// SHIPMENT / PO
	// ==================================================

	shipmentOriginal := strings.TrimSpace(
		r.FormValue("shipment"),
	)

	// Both fields are required (matches the client-side rule shown to the
	// user: "* Customer Name and Shipment / PO are both required").
	if customer == "" || shipmentOriginal == "" {

		writeJSON(
			w,
			http.StatusBadRequest,
			UploadResponse{
				Success: false,
				Message: "Customer Name and Shipment / PO are both required",
			},
		)

		return
	}

	safeCustomer := sanitizeValue(
		customer,
	)

	if safeCustomer == "" {

		writeJSON(
			w,
			http.StatusBadRequest,
			UploadResponse{
				Success: false,
				Message: "Invalid Customer Name",
			},
		)

		return
	}

	shipment := sanitizeValue(
		shipmentOriginal,
	)

	if shipment == "" {

		writeJSON(
			w,
			http.StatusBadRequest,
			UploadResponse{
				Success: false,
				Message: "Invalid Shipment / PO",
			},
		)

		return
	}

	// ==================================================
	// DESTINATION EMAIL
	// ==================================================

	destination := strings.TrimSpace(
		r.FormValue("destination"),
	)

	// V3 supports multiple recipients through repeated "destinations" form fields.
	// Keep the legacy single "destination" field as a fallback so the current
	// frontend remains fully compatible during the transition.
	destinations := uniqueValidDestinations(r.MultipartForm.Value["destinations"])
	if len(destinations) == 0 && destination != "" {
		destinations = uniqueValidDestinations([]string{destination})
	}

	if destination == "" && len(destinations) > 0 {
		destination = destinations[0]
	}

	if destination == "" {

		writeJSON(
			w,
			http.StatusBadRequest,
			UploadResponse{
				Success: false,
				Message: "Destination email is required",
			},
		)

		return
	}

	if len(destinations) == 0 {

		writeJSON(
			w,
			http.StatusBadRequest,
			UploadResponse{
				Success: false,
				Message: "Invalid destination email",
			},
		)

		return
	}

	// ==================================================
	// USER EMAIL
	// ==================================================

	userEmail := strings.TrimSpace(
		r.FormValue("user_email"),
	)

	if userEmail == "" {

		writeJSON(
			w,
			http.StatusBadRequest,
			UploadResponse{
				Success: false,
				Message: "User email is required",
			},
		)

		return
	}

	if !validEmail(userEmail) {

		writeJSON(
			w,
			http.StatusBadRequest,
			UploadResponse{
				Success: false,
				Message: "Invalid user email",
			},
		)

		return
	}

	// ==================================================
	// OPTIONAL NOTES / COMMENTS
	// ==================================================

	notes := strings.TrimSpace(r.FormValue("notes"))
	if len([]rune(notes)) > 500 {
		writeJSON(w, http.StatusBadRequest, UploadResponse{
			Success: false,
			Message: "Notes / Comments cannot exceed 500 characters",
		})
		return
	}

	// ==================================================
	// PHOTOS
	// ==================================================

	files := r.MultipartForm.File["photos"]

	if len(files) == 0 {

		writeJSON(
			w,
			http.StatusBadRequest,
			UploadResponse{
				Success: false,
				Message: "At least one photo is required",
			},
		)

		return
	}

	if len(files) > maxPhotos {

		writeJSON(
			w,
			http.StatusBadRequest,
			UploadResponse{
				Success: false,
				Message: "Maximum 4 photos per shipment",
			},
		)

		return
	}

	// ==================================================
	// CREATE UPLOAD DIRECTORY
	// ==================================================

	uploadDir := filepath.Join(
		"uploads",
		shipment,
	)

	if err := os.MkdirAll(
		uploadDir,
		0755,
	); err != nil {

		log.Println(
			"Unable to create upload directory:",
			err,
		)

		writeJSON(
			w,
			http.StatusInternalServerError,
			UploadResponse{
				Success: false,
				Message: "Unable to create shipment directory",
			},
		)

		return
	}

	// ==================================================
	// TIMESTAMP
	// ==================================================

	now := time.Now()

	timestamp := now.Format(
		"20060102_150405",
	)

	// ==================================================
	// SAVE PHOTOS
	// ==================================================

	saved := 0

	savedFiles := make(
		[]string,
		0,
		len(files),
	)

	for i, header := range files {

		// --------------------------------------------------
		// OPEN UPLOADED FILE
		// --------------------------------------------------

		src, err := header.Open()

		if err != nil {

			log.Println(
				"Unable to open photo:",
				err,
			)

			continue
		}

		// --------------------------------------------------
		// DETERMINE EXTENSION
		// --------------------------------------------------

		extension := strings.ToLower(
			filepath.Ext(
				header.Filename,
			),
		)

		if extension == "" {

			contentType := header.Header.Get(
				"Content-Type",
			)

			extensions, _ := mime.ExtensionsByType(
				contentType,
			)

			if len(extensions) > 0 {

				extension = extensions[0]

			} else {

				extension = ".jpg"
			}
		}

		// --------------------------------------------------
		// ALLOW COMMON IMAGE EXTENSIONS ONLY
		// --------------------------------------------------

		if !allowedImageExtension(
			extension,
		) {

			src.Close()

			log.Printf(
				"Rejected unsupported file: %s",
				header.Filename,
			)

			continue
		}

		// --------------------------------------------------
		// GENERATE FILENAME
		// --------------------------------------------------

		filename := fmt.Sprintf(
			"%s_%s_%s_%02d%s",
			safeCustomer,
			shipment,
			timestamp,
			i+1,
			extension,
		)

		destinationPath := filepath.Join(
			uploadDir,
			filename,
		)

		// --------------------------------------------------
		// CREATE DESTINATION FILE
		// --------------------------------------------------

		dst, err := os.Create(
			destinationPath,
		)

		if err != nil {

			src.Close()

			log.Println(
				"Unable to create photo:",
				err,
			)

			continue
		}

		// --------------------------------------------------
		// COPY PHOTO
		// --------------------------------------------------

		_, copyErr := io.Copy(
			dst,
			src,
		)

		dst.Close()
		src.Close()

		if copyErr != nil {

			os.Remove(
				destinationPath,
			)

			log.Println(
				"Unable to save photo:",
				copyErr,
			)

			continue
		}

		saved++

		savedFiles = append(
			savedFiles,
			destinationPath,
		)

		// --------------------------------------------------
		// WATERMARK (logo + "Powered by QuickQuoteTool.ca")
		// --------------------------------------------------
		// Stamped in place on the saved file, so the same watermarked
		// image is what later gets attached to the outgoing email. A
		// failure here is logged but never blocks the upload — the
		// un-watermarked photo is still better than no photo at all.

		if err := watermarkPhoto(destinationPath); err != nil {

			log.Println(
				"Unable to watermark photo:",
				err,
			)
		}

		log.Printf(
			"Customer: %s | Shipment: %s | Saved: %s",
			customer,
			shipmentOriginal,
			destinationPath,
		)
	}

	// ==================================================
	// VERIFY PHOTOS
	// ==================================================

	if saved == 0 {

		writeJSON(
			w,
			http.StatusInternalServerError,
			UploadResponse{
				Success: false,
				Message: "Unable to save photos",
			},
		)

		return
	}

	// ==================================================
	// PREPARE EMAIL
	// ==================================================

	customerDisplay := customer
	if customerDisplay == "" {
		customerDisplay = "N/A"
	}

	shipmentDisplay := shipmentOriginal
	if shipmentDisplay == "" {
		shipmentDisplay = "N/A"
	}

	emailSubject := fmt.Sprintf(
		"QuickProof - %s - Shipment %s",
		customerDisplay,
		shipmentDisplay,
	)

	notesText := ""
	notesHTML := ""
	if notes != "" {
		notesText = fmt.Sprintf("Notes / Comments:\n%s\n\n", notes)
		notesHTML = fmt.Sprintf(
			`<div style="margin-top:20px; padding:14px 16px; background:#f3f4f6; border-radius:10px;">
				<strong>Notes / Comments:</strong><br>
				<span style="white-space:pre-wrap;">%s</span>
			</div>`,
			htmlEscape(notes),
		)
	}

	emailBody := fmt.Sprintf(
		"QuickProof\n"+
			"Shipment Photos - Proof - Email - Archive\n\n"+
			"Customer: %s\n"+
			"Shipment / PO: %s\n"+
			"Submitted by: %s\n"+
			"Date: %s\n"+
			"Photos attached: %d\n\n"+
			"%s"+
			"The attached photos were submitted through QuickProof "+
			"for shipment documentation and record-keeping purposes.\n\n"+
			"QuickProof\n"+
			"Shipping Documentation System\n"+
			"quickquotetool.ca\n",
		customerDisplay,
		shipmentDisplay,
		userEmail,
		now.Format("2006-01-02 15:04:05"),
		saved,
		notesText,
	)

	emailHTML := fmt.Sprintf(
		`<!DOCTYPE html>
<html>
<head>
<meta name="color-scheme" content="light">
<meta name="supported-color-schemes" content="light">
</head>
<body style="margin:0; padding:0; font-family:Arial,Helvetica,sans-serif; color:#1f2937 !important; background:#ffffff !important;">
	<div style="max-width:600px; margin:0 auto; padding:24px; background:#ffffff !important;">
		<div style="text-align:center; margin-bottom:24px;">
			<img src="data:image/png;base64,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" width="72" height="72" alt="QuickProof" style="display:block; margin:0 auto 10px auto; border-radius:16px;">
			<h2 style="margin:0; font-size:26px;">
				<font color="#1f2937"><span style="color:#1f2937 !important; text-shadow:-1px -1px 0 rgba(0,0,0,.25), 1px -1px 0 rgba(0,0,0,.25), -1px 1px 0 rgba(0,0,0,.25), 1px 1px 0 rgba(0,0,0,.25);">Quick</span></font><font color="#017424"><span style="color:#017424 !important; text-shadow:-1px -1px 0 rgba(0,0,0,.25), 1px -1px 0 rgba(0,0,0,.25), -1px 1px 0 rgba(0,0,0,.25), 1px 1px 0 rgba(0,0,0,.25);">Proof</span></font>
			</h2>
			<p style="margin:6px 0 0 0; color:#6b7280; font-size:13px;">
				Shipment Photos<br>Proof &bull; Email &bull; Archive
			</p>
		</div>

		<table style="border-collapse:collapse; width:100%%;">
			<tr>
				<td style="padding:6px 12px 6px 0;"><strong>Customer:</strong></td>
				<td style="padding:6px 0;">%s</td>
			</tr>
			<tr>
				<td style="padding:6px 12px 6px 0;"><strong>Shipment / PO:</strong></td>
				<td style="padding:6px 0;">%s</td>
			</tr>
			<tr>
				<td style="padding:6px 12px 6px 0;"><strong>Submitted by:</strong></td>
				<td style="padding:6px 0;">%s</td>
			</tr>
			<tr>
				<td style="padding:6px 12px 6px 0;"><strong>Date:</strong></td>
				<td style="padding:6px 0;">%s</td>
			</tr>
			<tr>
				<td style="padding:6px 12px 6px 0;"><strong>Photos attached:</strong></td>
				<td style="padding:6px 0;">%d</td>
			</tr>
		</table>

		%s

		<p style="margin-top:24px;">
			The attached photos were submitted through QuickProof
			for shipment documentation and record-keeping purposes.
		</p>

		<hr style="border:0; border-top:1px solid #e5e7eb; margin:24px 0;">

		<p style="font-size:12px; color:#6b7280; text-align:center;">
			Powered by <font color="#017424"><a href="https://quickquotetool.ca" style="color:#017424 !important; font-weight:bold; text-decoration:none;">QuickQuoteTool</a></font>
		</p>
	</div>
</body>
</html>`,
		htmlEscape(customerDisplay),
		htmlEscape(shipmentDisplay),
		htmlEscape(userEmail),
		htmlEscape(now.Format("2006-01-02 15:04:05")),
		saved,
		notesHTML,
	)

	// ==================================================
	// LOG EMAIL INFORMATION
	// ==================================================

	log.Println(
		"========================================",
	)

	log.Println(
		"EMAIL READY",
	)

	log.Println(
		"TO:",
		strings.Join(destinations, ", "),
	)

	log.Println(
		"FROM:",
		fromEmail,
	)

	log.Println(
		"REPLY-TO:",
		userEmail,
	)

	log.Println(
		"SUBJECT:",
		emailSubject,
	)

	for _, file := range savedFiles {

		log.Println(
			"ATTACHMENT:",
			file,
		)
	}

	log.Println(
		"========================================",
	)

	// ==================================================
	// SEND EMAIL THROUGH MAILERSEND
	// ==================================================

	// Send ONE MailerSend message.
	// The first selected department is TO; any additional departments are CC.
	toRecipient := destinations[0]
	ccRecipients := destinations[1:]

	log.Println("TO:", toRecipient)
	if len(ccRecipients) > 0 {
		log.Println("CC:", strings.Join(ccRecipients, ", "))
	}

	messageID, err := sendMailerSendEmail(
		apiKey,
		fromEmail,
		toRecipient,
		ccRecipients,
		userEmail,
		emailSubject,
		emailBody,
		emailHTML,
		savedFiles,
	)

	if err != nil {

		log.Println(
			"MAILERSEND ERROR:",
			err,
		)

		log.Println(
			"Photos were NOT deleted because email sending failed.",
		)

		writeJSON(
			w,
			http.StatusBadGateway,
			UploadResponse{
				Success:      false,
				Message:      "Unable to send email: " + err.Error(),
				Customer:     customer,
				Shipment:     shipmentOriginal,
				Destination:  destination,
				Destinations: destinations,
				UserEmail:    userEmail,
				Photos:       saved,
			},
		)

		return
	}

	// ==================================================
	// MAILERSEND ACCEPTED EMAIL
	// ==================================================

	log.Println(
		"========================================",
	)

	log.Println(
		"MAILERSEND ACCEPTED EMAIL",
	)

	if messageID != "" {

		log.Println(
			"MESSAGE ID:",
			messageID,
		)
	}

	log.Println(
		"Deleting temporary photos...",
	)

	// ==================================================
	// DELETE PHOTOS IMMEDIATELY
	// ==================================================

	deleteErrors := false

	for _, file := range savedFiles {

		err := os.Remove(
			file,
		)

		if err != nil {

			deleteErrors = true

			log.Println(
				"Unable to delete temporary photo:",
				file,
				err,
			)

			continue
		}

		log.Println(
			"DELETED:",
			file,
		)
	}

	// ==================================================
	// REMOVE SHIPMENT DIRECTORY IF EMPTY
	// ==================================================

	err = os.Remove(
		uploadDir,
	)

	if err == nil {

		log.Println(
			"REMOVED EMPTY DIRECTORY:",
			uploadDir,
		)

	} else if !os.IsNotExist(err) {

		log.Println(
			"Shipment directory was not removed:",
			err,
		)
	}

	log.Println(
		"========================================",
	)

	// ==================================================
	// SUCCESS RESPONSE
	// ==================================================

	message := "Photos sent successfully"

	if deleteErrors {

		message = "Photos sent successfully, but some temporary files could not be deleted"
	}

	writeJSON(
		w,
		http.StatusOK,
		UploadResponse{
			Success:      true,
			Message:      message,
			Customer:     customer,
			Shipment:     shipmentOriginal,
			Destination:  destination,
			Destinations: destinations,
			UserEmail:    userEmail,
			Photos:       saved,
			MessageID:    messageID,
		},
	)
}

// ==================================================
// SEND EMAIL THROUGH MAILERSEND API
// ==================================================

func sendMailerSendEmail(
	apiKey string,
	fromEmail string,
	destination string,
	ccDestinations []string,
	replyTo string,
	subject string,
	textBody string,
	htmlBody string,
	files []string,
) (string, error) {

	// ==================================================
	// PREPARE ATTACHMENTS
	// ==================================================

	attachments := make(
		[]MailerSendAttachment,
		0,
		len(files),
	)

	for _, filePath := range files {

		data, err := os.ReadFile(
			filePath,
		)

		if err != nil {

			return "",
				fmt.Errorf(
					"unable to read attachment %s: %w",
					filepath.Base(filePath),
					err,
				)
		}

		encoded := base64.StdEncoding.EncodeToString(
			data,
		)

		attachments = append(
			attachments,
			MailerSendAttachment{
				Content:     encoded,
				Filename:    filepath.Base(filePath),
				Disposition: "attachment",
			},
		)
	}

	// ==================================================
	// BUILD MAILERSEND REQUEST
	// ==================================================

	payload := MailerSendRequest{
		From: MailerSendAddress{
			Email: fromEmail,
			Name:  "QuickProof",
		},

		To: []MailerSendAddress{
			{Email: destination},
		},

		Cc: buildMailerSendRecipients(ccDestinations),

		ReplyTo: MailerSendAddress{
			Email: replyTo,
		},

		Subject: subject,

		Text: textBody,

		HTML: htmlBody,

		Attachments: attachments,
	}

	// ==================================================
	// JSON
	// ==================================================

	jsonData, err := json.Marshal(
		payload,
	)

	if err != nil {

		return "",
			fmt.Errorf(
				"unable to create MailerSend request: %w",
				err,
			)
	}

	// ==================================================
	// HTTP REQUEST
	// ==================================================

	req, err := http.NewRequest(
		http.MethodPost,
		mailerSendURL,
		bytes.NewReader(jsonData),
	)

	if err != nil {

		return "",
			fmt.Errorf(
				"unable to create MailerSend HTTP request: %w",
				err,
			)
	}

	req.Header.Set(
		"Authorization",
		"Bearer "+apiKey,
	)

	req.Header.Set(
		"Content-Type",
		"application/json",
	)

	req.Header.Set(
		"Accept",
		"application/json",
	)

	// ==================================================
	// HTTP CLIENT
	// ==================================================

	client := &http.Client{
		Timeout: 60 * time.Second,
	}

	response, err := client.Do(
		req,
	)

	if err != nil {

		return "",
			fmt.Errorf(
				"unable to contact MailerSend: %w",
				err,
			)
	}

	defer response.Body.Close()

	// ==================================================
	// READ RESPONSE
	// ==================================================

	responseBody, err := io.ReadAll(
		io.LimitReader(
			response.Body,
			1<<20,
		),
	)

	if err != nil {

		return "",
			fmt.Errorf(
				"unable to read MailerSend response: %w",
				err,
			)
	}

	// ==================================================
	// MAILERSEND MUST RETURN 202 ACCEPTED
	// ==================================================

	if response.StatusCode != http.StatusAccepted {

		errorMessage := strings.TrimSpace(
			string(responseBody),
		)

		if errorMessage == "" {

			errorMessage = response.Status
		}

		return "",
			fmt.Errorf(
				"MailerSend returned HTTP %d: %s",
				response.StatusCode,
				errorMessage,
			)
	}

	// ==================================================
	// MESSAGE ID
	// ==================================================

	messageID := strings.TrimSpace(
		response.Header.Get(
			"X-Message-Id",
		),
	)

	return messageID, nil
}

// ==================================================
// MULTIPLE RECIPIENTS - V3
// ==================================================
func uniqueValidDestinations(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]bool)
	for _, value := range values {
		value = strings.TrimSpace(value)
		key := strings.ToLower(value)
		if value == "" || !validEmail(value) || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, value)
	}
	return result
}

func buildMailerSendRecipients(destinations []string) []MailerSendAddress {
	result := make([]MailerSendAddress, 0, len(destinations))
	for _, destination := range destinations {
		result = append(result, MailerSendAddress{Email: destination})
	}
	return result
}

// ==================================================
// EMAIL VALIDATION
// ==================================================

func validEmail(
	value string,
) bool {

	address, err := mail.ParseAddress(
		value,
	)

	if err != nil {
		return false
	}

	return strings.EqualFold(
		address.Address,
		value,
	)
}

// ==================================================
// ALLOWED IMAGE EXTENSIONS
// ==================================================

func allowedImageExtension(
	extension string,
) bool {

	switch strings.ToLower(extension) {

	case ".jpg",
		".jpeg",
		".png",
		".heic",
		".heif",
		".webp":

		return true

	default:

		return false
	}
}

// ==================================================
// SANITIZE FILE / DIRECTORY VALUE
// ==================================================

func sanitizeValue(
	value string,
) string {

	value = strings.TrimSpace(
		value,
	)

	re := regexp.MustCompile(
		`[^a-zA-Z0-9_-]+`,
	)

	value = re.ReplaceAllString(
		value,
		"_",
	)

	return strings.Trim(
		value,
		"_",
	)
}

// ==================================================
// BASIC HTML ESCAPE
// ==================================================

func htmlEscape(
	value string,
) string {

	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&#39;",
	)

	return replacer.Replace(
		value,
	)
}

// ==================================================
// JSON RESPONSE
// ==================================================

func writeJSON(
	w http.ResponseWriter,
	status int,
	response UploadResponse,
) {

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	w.WriteHeader(
		status,
	)

	if err := json.NewEncoder(w).Encode(
		response,
	); err != nil {

		log.Println(
			"JSON response error:",
			err,
		)
	}
}
