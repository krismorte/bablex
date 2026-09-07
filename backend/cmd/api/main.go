package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/google/uuid"
)

const (
	authModeAWS     = "aws"
	authModeLocal   = "local"
	accessTokenTTL  = time.Hour
	refreshTokenTTL = 30 * 24 * time.Hour
	passwordRounds  = 120000
)

type Book struct {
	ID           string `json:"id" dynamodbav:"id"`
	Title        string `json:"title" dynamodbav:"title"`
	AuthorID     string `json:"authorId,omitempty" dynamodbav:"authorId,omitempty"`
	Author       string `json:"author" dynamodbav:"author"`
	ISBN         string `json:"isbn,omitempty" dynamodbav:"isbn,omitempty"`
	CategoryID   string `json:"categoryId,omitempty" dynamodbav:"categoryId,omitempty"`
	Category     string `json:"category,omitempty" dynamodbav:"category,omitempty"`
	Status       string `json:"status" dynamodbav:"status"`
	Notes        string `json:"notes,omitempty" dynamodbav:"notes,omitempty"`
	RegisteredAt string `json:"registeredAt" dynamodbav:"registeredAt"`
	BoughtAt     string `json:"boughtAt,omitempty" dynamodbav:"boughtAt,omitempty"`
	ReadAt       string `json:"readAt,omitempty" dynamodbav:"readAt,omitempty"`
	PublishedAt  string `json:"publishedAt,omitempty" dynamodbav:"publishedAt,omitempty"`
	Language     string `json:"language,omitempty" dynamodbav:"language,omitempty"`
	Reaction     string `json:"reaction" dynamodbav:"reaction"`
	CreatedByID  string `json:"createdById,omitempty" dynamodbav:"createdById,omitempty"`
	CreatedAt    string `json:"createdAt" dynamodbav:"createdAt"`
	UpdatedAt    string `json:"updatedAt" dynamodbav:"updatedAt"`
}

type Comment struct {
	ID        string `json:"id" dynamodbav:"id"`
	UserID    string `json:"userId" dynamodbav:"userId"`
	UserName  string `json:"userName" dynamodbav:"userName"`
	UserEmail string `json:"userEmail" dynamodbav:"userEmail"`
	Text      string `json:"text" dynamodbav:"text"`
	CreatedAt string `json:"createdAt" dynamodbav:"createdAt"`
}

type storedBook struct {
	PK string `dynamodbav:"PK"`
	SK string `dynamodbav:"SK"`
	Book
}

type localUser struct {
	PK                string `dynamodbav:"PK"`
	SK                string `dynamodbav:"SK"`
	UserID            string `dynamodbav:"userId"`
	Email             string `dynamodbav:"email"`
	PasswordHash      string `dynamodbav:"passwordHash"`
	Name              string `dynamodbav:"name,omitempty"`
	BirthDate         string `dynamodbav:"birthDate,omitempty"`
	Country           string `dynamodbav:"country,omitempty"`
	PreferredLanguage string `dynamodbav:"preferredLanguage,omitempty"`
	CreatedAt         string `dynamodbav:"createdAt"`
}

type Library struct {
	ID          string `json:"id" dynamodbav:"id"`
	Name        string `json:"name" dynamodbav:"name"`
	Description string `json:"description,omitempty" dynamodbav:"description,omitempty"`
	OwnerID     string `json:"ownerId" dynamodbav:"ownerId"`
	OwnerEmail  string `json:"ownerEmail" dynamodbav:"ownerEmail"`
	CreatedAt   string `json:"createdAt" dynamodbav:"createdAt"`
	UpdatedAt   string `json:"updatedAt" dynamodbav:"updatedAt"`
}

type Author struct {
	ID        string `json:"id" dynamodbav:"id"`
	Name      string `json:"name" dynamodbav:"name"`
	Country   string `json:"country,omitempty" dynamodbav:"country,omitempty"`
	BirthDate string `json:"birthDate,omitempty" dynamodbav:"birthDate,omitempty"`
	CreatedAt string `json:"createdAt" dynamodbav:"createdAt"`
	UpdatedAt string `json:"updatedAt" dynamodbav:"updatedAt"`
}

type Category struct {
	ID   string `json:"id" dynamodbav:"id"`
	Name string `json:"name" dynamodbav:"name"`
}

type Profile struct {
	ID                string `json:"id"`
	Email             string `json:"email"`
	Name              string `json:"name,omitempty"`
	BirthDate         string `json:"birthDate,omitempty"`
	Country           string `json:"country,omitempty"`
	PreferredLanguage string `json:"preferredLanguage,omitempty"`
}

type principal struct{ ID, Email string }

type genericRow struct {
	PK string `dynamodbav:"PK"`
	SK string `dynamodbav:"SK"`
}

type createBookRequest struct {
	Title       string `json:"title"`
	AuthorID    string `json:"authorId"`
	Author      string `json:"author"`
	ISBN        string `json:"isbn"`
	CategoryID  string `json:"categoryId"`
	Category    string `json:"category"`
	Status      string `json:"status"`
	Notes       string `json:"notes"`
	BoughtAt    string `json:"boughtAt"`
	ReadAt      string `json:"readAt"`
	PublishedAt string `json:"publishedAt"`
	Language    string `json:"language"`
	Reaction    string `json:"reaction"`
}

type updateBookRequest struct {
	Title       *string `json:"title"`
	AuthorID    *string `json:"authorId"`
	Author      *string `json:"author"`
	ISBN        *string `json:"isbn"`
	CategoryID  *string `json:"categoryId"`
	Category    *string `json:"category"`
	Status      *string `json:"status"`
	Notes       *string `json:"notes"`
	BoughtAt    *string `json:"boughtAt"`
	ReadAt      *string `json:"readAt"`
	PublishedAt *string `json:"publishedAt"`
	Language    *string `json:"language"`
	Reaction    *string `json:"reaction"`
}

type createCommentRequest struct {
	Text string `json:"text"`
}

type signUpRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type refreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

type authResponse struct {
	AccessToken  string       `json:"accessToken"`
	RefreshToken string       `json:"refreshToken"`
	TokenType    string       `json:"tokenType"`
	ExpiresIn    int64        `json:"expiresIn"`
	User         localProfile `json:"user"`
}

type localProfile struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

type jwtClaims struct {
	Sub   string `json:"sub"`
	Email string `json:"email"`
	Type  string `json:"type"`
	Exp   int64  `json:"exp"`
}

type app struct {
	db              *dynamodb.Client
	tableName       string
	authMode        string
	localAuthSecret string
}

func main() {
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(envOr("AWS_REGION", "us-east-1")),
	)
	if err != nil {
		panic(err)
	}

	endpoint := strings.TrimSpace(os.Getenv("AWS_ENDPOINT_URL"))
	db := dynamodb.NewFromConfig(cfg, func(o *dynamodb.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
	})

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	a := &app{
		db:              db,
		tableName:       envOr("TABLE_NAME", "bablex"),
		authMode:        normalizeAuthMode(envOr("AUTH_MODE", authModeAWS)),
		localAuthSecret: envOr("LOCAL_AUTH_SECRET", "local-development-secret-change-me"),
	}

	if a.authMode == authModeLocal {
		if err := a.ensureLocalTable(ctx); err != nil {
			panic(err)
		}
		port := envOr("PORT", "8080")
		logServer(a, port)
		return
	}

	lambda.Start(a.handler)
}

func sourceIPAllowed(sourceIP string) bool {
	cidrs := strings.TrimSpace(os.Getenv("ALLOWED_SOURCE_CIDRS"))
	if cidrs == "" {
		return true
	}
	ip := net.ParseIP(strings.TrimSpace(sourceIP))
	if ip == nil {
		return false
	}
	for _, raw := range strings.Split(cidrs, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if _, network, err := net.ParseCIDR(raw); err == nil && network.Contains(ip) {
			return true
		}
	}
	return false
}

func normalizeAuthMode(mode string) string {
	if strings.EqualFold(mode, authModeLocal) {
		return authModeLocal
	}
	return authModeAWS
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func (a *app) handler(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	// In AWS mode the public IP restriction is enforced by AWS WAF on CloudFront.
	// Lambda is deliberately not given a second source-IP check because API Gateway
	// can report a different transport address (for example IPv6 vs IPv4).
	method := req.RequestContext.HTTP.Method
	path := req.RawPath
	if path == "" {
		path = req.RequestContext.HTTP.Path
	}
	path = "/" + strings.TrimPrefix(strings.TrimSpace(path), "/")
	path = strings.TrimPrefix(path, "/api")
	if path == "" {
		path = "/"
	}

	if method == http.MethodOptions {
		return corsResponse(http.StatusNoContent, nil), nil
	}
	if path == "/health" && method == http.MethodGet {
		return response(http.StatusOK, map[string]string{"status": "ok", "authMode": a.authMode}), nil
	}

	if a.authMode == authModeLocal {
		if path == "/auth/signup" && method == http.MethodPost {
			return a.localSignUp(ctx, req.Body)
		}
		if path == "/auth/login" && method == http.MethodPost {
			return a.localLogin(ctx, req.Body)
		}
		if path == "/auth/refresh" && method == http.MethodPost {
			return a.localRefresh(req.Body)
		}
	}

	p := principal{}
	if a.authMode == authModeLocal {
		c := a.localClaims(req)
		p = principal{ID: c.Sub, Email: c.Email}
	} else {
		p = principal{ID: claimsSub(req), Email: claimsEmail(req)}
	}
	if p.ID == "" {
		return response(http.StatusUnauthorized, map[string]string{"error": "unauthorized"}), nil
	}

	parts := splitPath(path)
	if len(parts) == 1 && parts[0] == "me" {
		switch method {
		case http.MethodGet:
			return a.getProfile(ctx, p)
		case http.MethodPatch:
			return a.updateProfile(ctx, p, req.Body)
		}
	}
	if len(parts) == 2 && parts[0] == "me" && parts[1] == "password" && method == http.MethodPost {
		return a.changeLocalPassword(ctx, p, req.Body)
	}
	if len(parts) == 1 && parts[0] == "libraries" {
		switch method {
		case http.MethodGet:
			return a.listLibraries(ctx, p)
		case http.MethodPost:
			return a.createLibrary(ctx, p, req.Body)
		}
	}
	if len(parts) == 2 && parts[0] == "libraries" {
		switch method {
		case http.MethodGet:
			return a.getLibrary(ctx, p, parts[1])
		case http.MethodPatch:
			return a.updateLibrary(ctx, p, parts[1], req.Body)
		}
	}
	if len(parts) == 3 && parts[0] == "libraries" && parts[2] == "access" && method == http.MethodGet {
		return a.listLibraryAccess(ctx, p, parts[1])
	}
	if len(parts) == 3 && parts[0] == "libraries" && parts[2] == "share" && method == http.MethodPost {
		return a.shareLibrary(ctx, p, parts[1], req.Body)
	}
	if len(parts) == 3 && parts[0] == "libraries" && parts[2] == "share" && method == http.MethodDelete {
		return a.unshareLibrary(ctx, p, parts[1], req.Body)
	}
	if len(parts) == 3 && parts[0] == "libraries" && parts[2] == "books" {
		switch method {
		case http.MethodGet:
			return a.listLibraryBooks(ctx, p, parts[1])
		case http.MethodPost:
			return a.createLibraryBook(ctx, p, parts[1], req.Body)
		}
	}
	if len(parts) == 4 && parts[0] == "libraries" && parts[2] == "books" {
		switch method {
		case http.MethodPatch:
			return a.updateLibraryBook(ctx, p, parts[1], parts[3], req.Body)
		case http.MethodDelete:
			return a.deleteLibraryBook(ctx, p, parts[1], parts[3])
		}
	}
	if len(parts) == 5 && parts[0] == "libraries" && parts[2] == "books" && parts[4] == "move" && method == http.MethodPost {
		return a.moveLibraryBook(ctx, p, parts[1], parts[3], req.Body)
	}
	if len(parts) == 5 && parts[0] == "libraries" && parts[2] == "books" && parts[4] == "comments" {
		switch method {
		case http.MethodGet:
			return a.listBookComments(ctx, p, parts[1], parts[3])
		case http.MethodPost:
			return a.createBookComment(ctx, p, parts[1], parts[3], req.Body)
		}
	}

	if len(parts) == 1 && parts[0] == "authors" {
		switch method {
		case http.MethodGet:
			return a.listAuthors(ctx, p.ID)
		case http.MethodPost:
			return a.createAuthor(ctx, p.ID, req.Body)
		}
	}
	if len(parts) == 2 && parts[0] == "authors" {
		switch method {
		case http.MethodGet:
			return a.getAuthor(ctx, p.ID, parts[1])
		case http.MethodPatch:
			return a.updateAuthor(ctx, p.ID, parts[1], req.Body)
		}
	}
	if len(parts) == 1 && parts[0] == "categories" {
		switch method {
		case http.MethodGet:
			return a.listCategories(ctx, p.ID)
		case http.MethodPost:
			return a.createCategory(ctx, p.ID, req.Body)
		}
	}
	if len(parts) == 2 && parts[0] == "categories" {
		switch method {
		case http.MethodGet:
			return a.getCategory(ctx, p.ID, parts[1])
		case http.MethodPatch:
			return a.updateCategory(ctx, p.ID, parts[1], req.Body)
		}
	}

	return response(http.StatusNotFound, map[string]string{"error": "not found"}), nil
}

func (a *app) ensureLocalTable(ctx context.Context) error {
	var lastErr error
	for attempt := 0; attempt < 30; attempt++ {
		_, err := a.db.DescribeTable(ctx, &dynamodb.DescribeTableInput{TableName: aws.String(a.tableName)})
		if err == nil {
			return nil
		}
		lastErr = err
		time.Sleep(time.Second)
	}

	_, err := a.db.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName:   aws.String(a.tableName),
		BillingMode: types.BillingModePayPerRequest,
		AttributeDefinitions: []types.AttributeDefinition{
			{AttributeName: aws.String("PK"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("SK"), AttributeType: types.ScalarAttributeTypeS},
		},
		KeySchema: []types.KeySchemaElement{
			{AttributeName: aws.String("PK"), KeyType: types.KeyTypeHash},
			{AttributeName: aws.String("SK"), KeyType: types.KeyTypeRange},
		},
	})
	if err != nil {
		if lastErr != nil {
			return fmt.Errorf("create local table after waiting for Kumo: %w (last describe error: %v)", err, lastErr)
		}
		return fmt.Errorf("create local table: %w", err)
	}

	return nil
}

func normalizePreferredLanguage(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "es", "pt", "fr", "de":
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return "en"
	}
}

func (a *app) localSignUp(ctx context.Context, body string) (events.APIGatewayV2HTTPResponse, error) {
	var in signUpRequest
	if err := json.NewDecoder(strings.NewReader(body)).Decode(&in); err != nil {
		return response(http.StatusBadRequest, map[string]string{"error": "invalid JSON"}), nil
	}
	in.Email = normalizeEmail(in.Email)
	if err := validateCredentials(in.Email, in.Password); err != nil {
		return response(http.StatusBadRequest, map[string]string{"error": err.Error()}), nil
	}

	key := map[string]types.AttributeValue{
		"PK": &types.AttributeValueMemberS{Value: "EMAIL#" + in.Email},
		"SK": &types.AttributeValueMemberS{Value: "PROFILE"},
	}
	check, err := a.db.GetItem(ctx, &dynamodb.GetItemInput{TableName: aws.String(a.tableName), Key: key})
	if err != nil {
		return response(http.StatusInternalServerError, map[string]string{"error": "failed to check account"}), err
	}
	if len(check.Item) > 0 {
		return response(http.StatusConflict, map[string]string{"error": "an account with this email already exists"}), nil
	}

	user := localUser{
		PK:                "EMAIL#" + in.Email,
		SK:                "PROFILE",
		UserID:            uuid.NewString(),
		Email:             in.Email,
		PasswordHash:      hashPassword(in.Password),
		PreferredLanguage: "en",
		CreatedAt:         time.Now().UTC().Format(time.RFC3339Nano),
	}
	item, err := attributevalue.MarshalMap(user)
	if err != nil {
		return response(http.StatusInternalServerError, map[string]string{"error": "failed to encode account"}), err
	}
	_, err = a.db.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String(a.tableName), Item: item, ConditionExpression: aws.String("attribute_not_exists(PK)")})
	if err != nil {
		return response(http.StatusInternalServerError, map[string]string{"error": "failed to create account"}), err
	}

	// Keep a profile row keyed by the stable user ID as well as the email lookup row.
	// The application API uses USER#<id> for profile and library ownership data.
	profile := struct {
		PK, SK, UserID, Email, Name, BirthDate, Country, CreatedAt string
	}{
		PK: "USER#" + user.UserID, SK: "PROFILE", UserID: user.UserID, Email: user.Email, CreatedAt: user.CreatedAt,
	}
	if err := a.put(ctx, profile); err != nil {
		return response(http.StatusInternalServerError, map[string]string{"error": "failed to create profile"}), err
	}

	if _, err := a.ensureDefaultLibrary(ctx, principal{ID: user.UserID, Email: user.Email}); err != nil {
		return response(http.StatusInternalServerError, map[string]string{"error": "failed to create default library"}), err
	}

	return response(http.StatusCreated, map[string]any{
		"user":    localProfile{ID: user.UserID, Email: user.Email},
		"message": "account created; you can now sign in",
	}), nil
}

func (a *app) localLogin(ctx context.Context, body string) (events.APIGatewayV2HTTPResponse, error) {
	var in loginRequest
	if err := json.NewDecoder(strings.NewReader(body)).Decode(&in); err != nil {
		return response(http.StatusBadRequest, map[string]string{"error": "invalid JSON"}), nil
	}
	in.Email = normalizeEmail(in.Email)
	if in.Email == "" || in.Password == "" {
		return response(http.StatusUnauthorized, map[string]string{"error": "invalid email or password"}), nil
	}

	out, err := a.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(a.tableName),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: "EMAIL#" + in.Email},
			"SK": &types.AttributeValueMemberS{Value: "PROFILE"},
		},
	})
	if err != nil {
		return response(http.StatusInternalServerError, map[string]string{"error": "failed to load account"}), err
	}
	if len(out.Item) == 0 {
		return response(http.StatusUnauthorized, map[string]string{"error": "invalid email or password"}), nil
	}

	var user localUser
	if err := attributevalue.UnmarshalMap(out.Item, &user); err != nil {
		return response(http.StatusInternalServerError, map[string]string{"error": "failed to decode account"}), err
	}
	if !verifyPassword(in.Password, user.PasswordHash) {
		return response(http.StatusUnauthorized, map[string]string{"error": "invalid email or password"}), nil
	}

	return response(http.StatusOK, a.issueTokens(user.UserID, user.Email)), nil
}

func (a *app) localRefresh(body string) (events.APIGatewayV2HTTPResponse, error) {
	var in refreshRequest
	if err := json.NewDecoder(strings.NewReader(body)).Decode(&in); err != nil {
		return response(http.StatusBadRequest, map[string]string{"error": "invalid JSON"}), nil
	}
	claims, ok := a.verifyToken(in.RefreshToken, "refresh", false)
	if !ok {
		return response(http.StatusUnauthorized, map[string]string{"error": "invalid refresh token"}), nil
	}
	return response(http.StatusOK, authResponse{
		AccessToken:  a.signToken(jwtClaims{Sub: claims.Sub, Email: claims.Email, Type: "access", Exp: time.Now().Add(accessTokenTTL).Unix()}),
		RefreshToken: in.RefreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    int64(accessTokenTTL.Seconds()),
		User:         localProfile{ID: claims.Sub, Email: claims.Email},
	}), nil
}

func (a *app) issueTokens(userID, email string) authResponse {
	return authResponse{
		AccessToken: a.signToken(jwtClaims{
			Sub: userID, Email: email, Type: "access", Exp: time.Now().Add(accessTokenTTL).Unix(),
		}),
		RefreshToken: a.signToken(jwtClaims{
			Sub: userID, Email: email, Type: "refresh", Exp: time.Now().Add(refreshTokenTTL).Unix(),
		}),
		TokenType: "Bearer",
		ExpiresIn: int64(accessTokenTTL.Seconds()),
		User:      localProfile{ID: userID, Email: email},
	}
}

func (a *app) localClaims(req events.APIGatewayV2HTTPRequest) jwtClaims {
	header := req.Headers["Authorization"]
	if header == "" {
		header = req.Headers["authorization"]
	}
	if !strings.HasPrefix(strings.ToLower(header), "bearer ") {
		return jwtClaims{}
	}
	claims, ok := a.verifyToken(strings.TrimSpace(header[len("Bearer "):]), "access", false)
	if !ok {
		return jwtClaims{}
	}
	return claims
}

func (a *app) signToken(claims jwtClaims) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payloadBytes, _ := json.Marshal(claims)
	payload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	unsigned := header + "." + payload
	mac := hmac.New(sha256.New, []byte(a.localAuthSecret))
	_, _ = mac.Write([]byte(unsigned))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return unsigned + "." + signature
}

func (a *app) verifyToken(token, expectedType string, allowExpired bool) (jwtClaims, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return jwtClaims{}, false
	}
	unsigned := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, []byte(a.localAuthSecret))
	_, _ = mac.Write([]byte(unsigned))
	expectedSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expectedSig), []byte(parts[2])) {
		return jwtClaims{}, false
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return jwtClaims{}, false
	}
	var claims jwtClaims
	if json.Unmarshal(payload, &claims) != nil || claims.Sub == "" || claims.Type != expectedType {
		return jwtClaims{}, false
	}
	if !allowExpired && time.Now().Unix() >= claims.Exp {
		return jwtClaims{}, false
	}
	if allowExpired && time.Now().Unix() >= claims.Exp+refreshTokenTTL.Milliseconds()/1000 {
		return jwtClaims{}, false
	}
	return claims, true
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func validateCredentials(email, password string) error {
	if !strings.Contains(email, "@") || len(email) > 254 {
		return errors.New("a valid email is required")
	}
	if len(password) < 8 || len(password) > 128 {
		return errors.New("password must be between 8 and 128 characters")
	}
	return nil
}

func hashPassword(password string) string {
	saltBytes := make([]byte, 16)
	if _, err := rand.Read(saltBytes); err != nil {
		panic(err)
	}
	digest := sha256.Sum256(append(saltBytes, []byte(password)...))
	for i := 1; i < passwordRounds; i++ {
		next := sha256.Sum256(append(digest[:], saltBytes...))
		digest = next
	}
	return hex.EncodeToString(saltBytes) + "$" + hex.EncodeToString(digest[:])
}

func verifyPassword(password, stored string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 2 {
		return false
	}
	salt, err1 := hex.DecodeString(parts[0])
	expected, err2 := hex.DecodeString(parts[1])
	if err1 != nil || err2 != nil || len(expected) != sha256.Size {
		return false
	}
	digest := sha256.Sum256(append(salt, []byte(password)...))
	for i := 1; i < passwordRounds; i++ {
		next := sha256.Sum256(append(digest[:], salt...))
		digest = next
	}
	return hmac.Equal(digest[:], expected)
}

func logServer(a *app, port string) {
	httpHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" && r.Method == http.MethodGet {
			writeHTTPResponse(w, http.StatusOK, map[string]string{"status": "ok", "authMode": a.authMode})
			return
		}

		bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
		if err != nil {
			writeHTTPResponse(w, http.StatusBadRequest, map[string]string{"error": "failed to read request"})
			return
		}
		req := events.APIGatewayV2HTTPRequest{
			Version:        "2.0",
			RawPath:        r.URL.Path,
			RawQueryString: r.URL.RawQuery,
			Headers:        map[string]string{},
			Body:           string(bodyBytes),
			RequestContext: events.APIGatewayV2HTTPRequestContext{
				HTTP: events.APIGatewayV2HTTPRequestContextHTTPDescription{Method: r.Method, Path: r.URL.Path},
			},
		}
		for key, values := range r.Header {
			if len(values) > 0 {
				req.Headers[key] = values[0]
			}
		}

		resp, err := a.handler(r.Context(), req)
		if err != nil {
			writeHTTPResponse(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}
		for key, value := range resp.Headers {
			w.Header().Set(key, value)
		}
		writeHTTPResponse(w, resp.StatusCode, responseBody(resp.Body))
	})

	slog.Info("local API listening", "port", port, "authMode", a.authMode, "table", a.tableName)
	server := &http.Server{Addr: ":" + port, Handler: withCORS(httpHandler)}
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		panic(err)
	}
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Add("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func responseBody(body string) any {
	if strings.TrimSpace(body) == "" {
		return nil
	}
	var payload any
	if json.Unmarshal([]byte(body), &payload) == nil {
		return payload
	}
	return map[string]string{"message": body}
}

func writeHTTPResponse(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if payload != nil {
		_ = json.NewEncoder(w).Encode(payload)
	}
}

func (a *app) listBooks(ctx context.Context, userID string) (events.APIGatewayV2HTTPResponse, error) {
	out, err := a.db.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(a.tableName),
		KeyConditionExpression: aws.String("PK = :pk AND begins_with(SK, :prefix)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk":     &types.AttributeValueMemberS{Value: "USER#" + userID},
			":prefix": &types.AttributeValueMemberS{Value: "BOOK#"},
		},
	})
	if err != nil {
		return response(http.StatusInternalServerError, map[string]string{"error": "failed to list books"}), err
	}

	books := make([]Book, 0, len(out.Items))
	for _, item := range out.Items {
		var row storedBook
		if err := attributevalue.UnmarshalMap(item, &row); err != nil {
			return response(http.StatusInternalServerError, map[string]string{"error": "failed to decode books"}), err
		}
		books = append(books, row.Book)
	}

	return response(http.StatusOK, map[string]any{"items": books}), nil
}

func (a *app) getBook(ctx context.Context, userID, bookID string) (events.APIGatewayV2HTTPResponse, error) {
	out, err := a.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(a.tableName),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: "USER#" + userID},
			"SK": &types.AttributeValueMemberS{Value: "BOOK#" + bookID},
		},
	})
	if err != nil {
		return response(http.StatusInternalServerError, map[string]string{"error": "failed to get book"}), err
	}
	if len(out.Item) == 0 {
		return response(http.StatusNotFound, map[string]string{"error": "book not found"}), nil
	}

	var row storedBook
	if err := attributevalue.UnmarshalMap(out.Item, &row); err != nil {
		return response(http.StatusInternalServerError, map[string]string{"error": "failed to decode book"}), err
	}
	return response(http.StatusOK, row.Book), nil
}

func (a *app) createBook(ctx context.Context, userID, body string) (events.APIGatewayV2HTTPResponse, error) {
	var in createBookRequest
	if err := json.NewDecoder(strings.NewReader(body)).Decode(&in); err != nil {
		return response(http.StatusBadRequest, map[string]string{"error": "invalid JSON"}), nil
	}
	in.Title = strings.TrimSpace(in.Title)
	in.Author = strings.TrimSpace(in.Author)
	if in.Title == "" {
		return response(http.StatusBadRequest, map[string]string{"error": "title is required"}), nil
	}

	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = "owned"
	}
	if !validStatus(status) {
		return response(http.StatusBadRequest, map[string]string{"error": "invalid status"}), nil
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	book := Book{
		ID: uuid.NewString(), Title: in.Title, Author: in.Author, ISBN: strings.TrimSpace(in.ISBN),
		Category: strings.TrimSpace(in.Category), Status: status, Notes: strings.TrimSpace(in.Notes),
		PublishedAt: normalizeDate(in.PublishedAt), CreatedAt: now, UpdatedAt: now,
	}
	row := storedBook{PK: "USER#" + userID, SK: "BOOK#" + book.ID, Book: book}

	item, err := attributevalue.MarshalMap(row)
	if err != nil {
		return response(http.StatusInternalServerError, map[string]string{"error": "failed to encode book"}), err
	}
	_, err = a.db.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String(a.tableName), Item: item, ConditionExpression: aws.String("attribute_not_exists(PK) AND attribute_not_exists(SK)")})
	if err != nil {
		return response(http.StatusInternalServerError, map[string]string{"error": "failed to create book"}), err
	}
	return response(http.StatusCreated, book), nil
}

func (a *app) updateBook(ctx context.Context, userID, bookID, body string) (events.APIGatewayV2HTTPResponse, error) {
	var in updateBookRequest
	if err := json.NewDecoder(strings.NewReader(body)).Decode(&in); err != nil {
		return response(http.StatusBadRequest, map[string]string{"error": "invalid JSON"}), nil
	}

	currentResp, err := a.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(a.tableName),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: "USER#" + userID},
			"SK": &types.AttributeValueMemberS{Value: "BOOK#" + bookID},
		},
	})
	if err != nil {
		return response(http.StatusInternalServerError, map[string]string{"error": "failed to get book"}), err
	}
	if len(currentResp.Item) == 0 {
		return response(http.StatusNotFound, map[string]string{"error": "book not found"}), nil
	}

	var row storedBook
	if err := attributevalue.UnmarshalMap(currentResp.Item, &row); err != nil {
		return response(http.StatusInternalServerError, map[string]string{"error": "failed to decode book"}), err
	}

	if in.Title != nil {
		row.Title = strings.TrimSpace(*in.Title)
	}
	if in.Author != nil {
		row.Author = strings.TrimSpace(*in.Author)
	}
	if in.ISBN != nil {
		row.ISBN = strings.TrimSpace(*in.ISBN)
	}
	if in.Category != nil {
		row.Category = strings.TrimSpace(*in.Category)
	}
	if in.Status != nil {
		row.Status = strings.TrimSpace(*in.Status)
	}
	if in.Notes != nil {
		row.Notes = strings.TrimSpace(*in.Notes)
	}
	if row.Title == "" {
		return response(http.StatusBadRequest, map[string]string{"error": "title is required"}), nil
	}
	if !validStatus(row.Status) {
		return response(http.StatusBadRequest, map[string]string{"error": "invalid status"}), nil
	}
	row.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)

	item, err := attributevalue.MarshalMap(row)
	if err != nil {
		return response(http.StatusInternalServerError, map[string]string{"error": "failed to encode book"}), err
	}
	_, err = a.db.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String(a.tableName), Item: item})
	if err != nil {
		return response(http.StatusInternalServerError, map[string]string{"error": "failed to update book"}), err
	}
	return response(http.StatusOK, row.Book), nil
}

func (a *app) deleteBook(ctx context.Context, userID, bookID string) (events.APIGatewayV2HTTPResponse, error) {
	_, err := a.db.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(a.tableName),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: "USER#" + userID},
			"SK": &types.AttributeValueMemberS{Value: "BOOK#" + bookID},
		},
	})
	if err != nil {
		return response(http.StatusInternalServerError, map[string]string{"error": "failed to delete book"}), err
	}
	if err := a.deleteBookComments(ctx, bookID); err != nil {
		return response(http.StatusInternalServerError, map[string]string{"error": "book deleted but comments could not be cleaned up"}), err
	}
	return response(http.StatusNoContent, nil), nil
}

func (a *app) put(ctx context.Context, value any) error {
	item, err := attributevalue.MarshalMap(value)
	if err != nil {
		return err
	}
	_, err = a.db.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String(a.tableName), Item: item})
	return err
}
func (a *app) get(ctx context.Context, pk, sk string, out any) (bool, error) {
	r, err := a.db.GetItem(ctx, &dynamodb.GetItemInput{TableName: aws.String(a.tableName), Key: map[string]types.AttributeValue{"PK": &types.AttributeValueMemberS{Value: pk}, "SK": &types.AttributeValueMemberS{Value: sk}}})
	if err != nil {
		return false, err
	}
	if len(r.Item) == 0 {
		return false, nil
	}
	return true, attributevalue.UnmarshalMap(r.Item, out)
}
func (a *app) queryPrefix(ctx context.Context, pk, prefix string) ([]map[string]types.AttributeValue, error) {
	r, err := a.db.Query(ctx, &dynamodb.QueryInput{TableName: aws.String(a.tableName), KeyConditionExpression: aws.String("PK = :pk AND begins_with(SK, :sk)"), ExpressionAttributeValues: map[string]types.AttributeValue{":pk": &types.AttributeValueMemberS{Value: pk}, ":sk": &types.AttributeValueMemberS{Value: prefix}}})
	if err != nil {
		return nil, err
	}
	return r.Items, nil
}

func (a *app) ensureDefaultLibrary(ctx context.Context, p principal) (Library, error) {
	items, err := a.queryPrefix(ctx, "USER#"+p.ID, "LIBRARY#")
	if err == nil && len(items) > 0 {
		for _, item := range items {
			var ref struct {
				LibraryID string `dynamodbav:"LibraryID"`
			}
			if attributevalue.UnmarshalMap(item, &ref) != nil || strings.TrimSpace(ref.LibraryID) == "" {
				continue
			}
			var l Library
			if ok, _ := a.get(ctx, "LIBRARY#"+ref.LibraryID, "META", &l); ok {
				return l, nil
			}
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	l := Library{ID: uuid.NewString(), Name: "My Library", Description: "My personal collection", OwnerID: p.ID, OwnerEmail: p.Email, CreatedAt: now, UpdatedAt: now}
	if err := a.put(ctx, struct {
		PK, SK  string
		Library `dynamodbav:",inline"`
	}{"LIBRARY#" + l.ID, "META", l}); err != nil {
		return l, err
	}
	_ = a.put(ctx, struct{ PK, SK, LibraryID string }{"USER#" + p.ID, "LIBRARY#" + l.ID, l.ID})
	return l, nil
}
func (a *app) canAccess(ctx context.Context, p principal, id string) (Library, bool) {
	var l Library
	ok, _ := a.get(ctx, "LIBRARY#"+id, "META", &l)
	if !ok {
		return l, false
	}
	if l.OwnerID == p.ID {
		return l, true
	}
	var x genericRow
	ok, _ = a.get(ctx, "LIBRARY#"+id, "ACCESS#"+normalizeEmail(p.Email), &x)
	return l, ok
}

type libraryView struct {
	Library
	OwnerName string `json:"ownerName,omitempty"`
	IsShared  bool   `json:"isShared"`
}

func (a *app) profileName(ctx context.Context, userID, fallback string) string {
	var row struct{ PK, SK, UserID, Email, Name string }
	if ok, _ := a.get(ctx, "USER#"+userID, "PROFILE", &row); ok && strings.TrimSpace(row.Name) != "" {
		return strings.TrimSpace(row.Name)
	}
	return fallback
}

func (a *app) listLibraries(ctx context.Context, p principal) (events.APIGatewayV2HTTPResponse, error) {
	ids := map[string]bool{}
	email := normalizeEmail(p.Email)
	for _, pk := range []string{"USER#" + p.ID, "EMAIL#" + email} {
		items, err := a.queryPrefix(ctx, pk, "LIBRARY#")
		if err != nil {
			return response(500, map[string]string{"error": "failed to list libraries"}), err
		}
		for _, it := range items {
			var r struct {
				LibraryID string `dynamodbav:"LibraryID"`
			}
			_ = attributevalue.UnmarshalMap(it, &r)
			if r.LibraryID == "" {
				if sk, ok := it["SK"].(*types.AttributeValueMemberS); ok {
					r.LibraryID = strings.TrimPrefix(sk.Value, "LIBRARY#")
				}
			}
			if r.LibraryID != "" {
				ids[r.LibraryID] = true
			}
		}
	}
	out := []libraryView{}
	for id := range ids {
		var l Library
		if ok, _ := a.get(ctx, "LIBRARY#"+id, "META", &l); ok {
			shared := l.OwnerID != p.ID
			ownerName := l.OwnerEmail
			if l.OwnerID == p.ID {
				ownerName = p.Email
			} else {
				ownerName = a.profileName(ctx, l.OwnerID, l.OwnerEmail)
			}
			out = append(out, libraryView{Library: l, OwnerName: ownerName, IsShared: shared})
		}
	}
	// Older Bablex versions accidentally created the default library on every login.
	// Keep only the oldest instance of that exact autogenerated library shape.
	filtered := make([]libraryView, 0, len(out))
	defaultKept := false
	var defaultLibrary libraryView
	for _, item := range out {
		isAutoDefault := !item.IsShared && strings.EqualFold(strings.TrimSpace(item.Name), "My Library") && strings.TrimSpace(item.Description) == "My personal collection" && item.OwnerID == p.ID
		if isAutoDefault {
			if !defaultKept || item.CreatedAt < defaultLibrary.CreatedAt {
				defaultLibrary = item
			}
			defaultKept = true
			continue
		}
		filtered = append(filtered, item)
	}
	if defaultKept {
		filtered = append(filtered, defaultLibrary)
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].IsShared != filtered[j].IsShared {
			return !filtered[i].IsShared
		}
		return strings.ToLower(filtered[i].Name) < strings.ToLower(filtered[j].Name)
	})
	return response(200, map[string]any{"items": filtered}), nil
}
func (a *app) createLibrary(ctx context.Context, p principal, body string) (events.APIGatewayV2HTTPResponse, error) {
	var in struct{ Name, Description string }
	if json.Unmarshal([]byte(body), &in) != nil || strings.TrimSpace(in.Name) == "" {
		return response(400, map[string]string{"error": "name is required"}), nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	l := Library{ID: uuid.NewString(), Name: strings.TrimSpace(in.Name), Description: strings.TrimSpace(in.Description), OwnerID: p.ID, OwnerEmail: p.Email, CreatedAt: now, UpdatedAt: now}
	if err := a.put(ctx, struct {
		PK, SK  string
		Library `dynamodbav:",inline"`
	}{"LIBRARY#" + l.ID, "META", l}); err != nil {
		return response(500, nil), err
	}
	if err := a.put(ctx, struct{ PK, SK, LibraryID string }{"USER#" + p.ID, "LIBRARY#" + l.ID, l.ID}); err != nil {
		// The metadata write succeeded; surface the indexing failure instead of pretending
		// the library was fully created.
		return response(500, map[string]string{"error": "failed to index library for user"}), err
	}
	return response(201, l), nil
}
func (a *app) getLibrary(ctx context.Context, p principal, id string) (events.APIGatewayV2HTTPResponse, error) {
	l, ok := a.canAccess(ctx, p, id)
	if !ok {
		return response(404, map[string]string{"error": "library not found"}), nil
	}
	return response(200, l), nil
}
func (a *app) updateLibrary(ctx context.Context, p principal, id, body string) (events.APIGatewayV2HTTPResponse, error) {
	l, ok := a.canAccess(ctx, p, id)
	if !ok || l.OwnerID != p.ID {
		return response(403, map[string]string{"error": "only the owner can edit this library"}), nil
	}
	var in struct{ Name, Description *string }
	if json.Unmarshal([]byte(body), &in) != nil {
		return response(400, nil), nil
	}
	if in.Name != nil {
		l.Name = strings.TrimSpace(*in.Name)
	}
	if in.Description != nil {
		l.Description = strings.TrimSpace(*in.Description)
	}
	if l.Name == "" {
		return response(400, map[string]string{"error": "name is required"}), nil
	}
	l.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	err := a.put(ctx, struct {
		PK, SK  string
		Library `dynamodbav:",inline"`
	}{"LIBRARY#" + id, "META", l})
	return response(200, l), err
}
func (a *app) shareLibrary(ctx context.Context, p principal, id, body string) (events.APIGatewayV2HTTPResponse, error) {
	l, ok := a.canAccess(ctx, p, id)
	if !ok || l.OwnerID != p.ID {
		return response(403, map[string]string{"error": "only the owner can share this library"}), nil
	}
	var in struct {
		Email string `json:"email"`
	}
	if json.Unmarshal([]byte(body), &in) != nil {
		return response(400, nil), nil
	}
	email := normalizeEmail(in.Email)
	if !strings.Contains(email, "@") {
		return response(400, map[string]string{"error": "valid email required"}), nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	row := struct{ PK, SK, Email, Role, CreatedAt string }{"LIBRARY#" + id, "ACCESS#" + email, email, "member", now}
	if err := a.put(ctx, row); err != nil {
		return response(500, nil), err
	}
	_ = a.put(ctx, struct{ PK, SK, LibraryID string }{"EMAIL#" + email, "LIBRARY#" + id, id})
	return response(201, map[string]string{"email": email, "role": "member"}), nil
}
func (a *app) unshareLibrary(ctx context.Context, p principal, id, body string) (events.APIGatewayV2HTTPResponse, error) {
	l, ok := a.canAccess(ctx, p, id)
	if !ok || l.OwnerID != p.ID {
		return response(http.StatusForbidden, map[string]string{"error": "only the owner can remove library access"}), nil
	}
	var in struct {
		Email string `json:"email"`
	}
	if json.Unmarshal([]byte(body), &in) != nil {
		return response(http.StatusBadRequest, map[string]string{"error": "valid email required"}), nil
	}
	email := normalizeEmail(in.Email)
	if email == "" || email == normalizeEmail(l.OwnerEmail) {
		return response(http.StatusBadRequest, map[string]string{"error": "owner access cannot be removed"}), nil
	}
	var accessRow struct {
		PK, SK, Email, Role, CreatedAt string
	}
	accessExists, err := a.get(ctx, "LIBRARY#"+id, "ACCESS#"+email, &accessRow)
	if err != nil {
		return response(http.StatusInternalServerError, map[string]string{"error": "failed to inspect library access"}), err
	}
	if !accessExists {
		return response(http.StatusNotFound, map[string]string{"error": "library access not found"}), nil
	}
	if err := a.deleteItemByKey(ctx, "LIBRARY#"+id, "ACCESS#"+email); err != nil {
		return response(http.StatusInternalServerError, map[string]string{"error": "failed to remove library access"}), err
	}
	if err := a.deleteItemByKey(ctx, "EMAIL#"+email, "LIBRARY#"+id); err != nil {
		_ = a.put(ctx, accessRow)
		return response(http.StatusInternalServerError, map[string]string{"error": "failed to remove library index; access restored"}), err
	}
	return response(http.StatusNoContent, nil), nil
}

func (a *app) deleteItemByKey(ctx context.Context, pk, sk string) error {
	_, err := a.db.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(a.tableName),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: pk},
			"SK": &types.AttributeValueMemberS{Value: sk},
		},
	})
	return err
}

func (a *app) listLibraryAccess(ctx context.Context, p principal, id string) (events.APIGatewayV2HTTPResponse, error) {
	l, ok := a.canAccess(ctx, p, id)
	if !ok {
		return response(404, nil), nil
	}
	items, err := a.queryPrefix(ctx, "LIBRARY#"+id, "ACCESS#")
	if err != nil {
		return response(500, nil), err
	}
	out := []map[string]string{{"email": normalizeEmail(l.OwnerEmail), "role": "owner"}}
	seen := map[string]bool{normalizeEmail(l.OwnerEmail): true}
	for _, it := range items {
		var r struct {
			Email string `dynamodbav:"Email"`
			Role  string `dynamodbav:"Role"`
		}
		_ = attributevalue.UnmarshalMap(it, &r)
		email := normalizeEmail(r.Email)
		// Older local data may have an ACCESS# item without the Email attribute.
		// Recover the address from the sort key instead of rendering an empty row.
		if email == "" {
			if sk, ok := it["SK"].(*types.AttributeValueMemberS); ok {
				email = normalizeEmail(strings.TrimPrefix(sk.Value, "ACCESS#"))
			}
		}
		if email == "" || !strings.Contains(email, "@") || seen[email] {
			continue
		}
		role := strings.TrimSpace(r.Role)
		if role == "" {
			role = "member"
		}
		seen[email] = true
		out = append(out, map[string]string{"email": email, "role": role})
	}
	return response(200, map[string]any{"items": out}), nil
}

func (a *app) bookCommentUser(ctx context.Context, p principal) (string, error) {
	var row struct {
		Name string `dynamodbav:"Name"`
	}
	ok, err := a.get(ctx, "USER#"+p.ID, "PROFILE", &row)
	if err != nil {
		return "", err
	}
	if ok && strings.TrimSpace(row.Name) != "" {
		return strings.TrimSpace(row.Name), nil
	}
	return p.Email, nil
}

func commentPartitionKey(bookID string) string {
	return "BOOK#" + strings.TrimSpace(bookID)
}

func commentSortKey(createdAt, id string) string {
	return "COMMENT#" + createdAt + "#" + id
}

func (a *app) listBookComments(ctx context.Context, p principal, libID, bookID string) (events.APIGatewayV2HTTPResponse, error) {
	if _, ok := a.canAccess(ctx, p, libID); !ok {
		return response(http.StatusForbidden, map[string]string{"error": "no library access"}), nil
	}
	var book storedBook
	ok, err := a.get(ctx, "LIBRARY#"+libID, "BOOK#"+bookID, &book)
	if err != nil {
		return response(http.StatusInternalServerError, map[string]string{"error": "failed to get book"}), err
	}
	if !ok || !isCurrentBook(book.Book) {
		return response(http.StatusNotFound, map[string]string{"error": "book not found"}), nil
	}
	items, err := a.queryPrefix(ctx, commentPartitionKey(bookID), "COMMENT#")
	if err != nil {
		return response(http.StatusInternalServerError, map[string]string{"error": "failed to list comments"}), err
	}
	out := make([]Comment, 0, len(items))
	for _, it := range items {
		var c Comment
		if attributevalue.UnmarshalMap(it, &c) != nil || strings.TrimSpace(c.Text) == "" {
			continue
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return response(http.StatusOK, map[string]any{"items": out}), nil
}

func (a *app) createBookComment(ctx context.Context, p principal, libID, bookID, body string) (events.APIGatewayV2HTTPResponse, error) {
	if _, ok := a.canAccess(ctx, p, libID); !ok {
		return response(http.StatusForbidden, map[string]string{"error": "no library access"}), nil
	}
	var book storedBook
	ok, err := a.get(ctx, "LIBRARY#"+libID, "BOOK#"+bookID, &book)
	if err != nil {
		return response(http.StatusInternalServerError, map[string]string{"error": "failed to get book"}), err
	}
	if !ok || !isCurrentBook(book.Book) {
		return response(http.StatusNotFound, map[string]string{"error": "book not found"}), nil
	}
	var in createCommentRequest
	if err := json.Unmarshal([]byte(body), &in); err != nil || strings.TrimSpace(in.Text) == "" {
		return response(http.StatusBadRequest, map[string]string{"error": "comment text is required"}), nil
	}
	userName, err := a.bookCommentUser(ctx, p)
	if err != nil {
		return response(http.StatusInternalServerError, map[string]string{"error": "failed to load user profile"}), err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	c := Comment{ID: uuid.NewString(), UserID: p.ID, UserName: userName, UserEmail: p.Email, Text: strings.TrimSpace(in.Text), CreatedAt: now}
	row := struct {
		PK, SK  string
		Comment `dynamodbav:",inline"`
	}{commentPartitionKey(bookID), commentSortKey(now, c.ID), c}
	if err := a.put(ctx, row); err != nil {
		return response(http.StatusInternalServerError, map[string]string{"error": "failed to save comment"}), err
	}
	return response(http.StatusCreated, c), nil
}

func (a *app) listLibraryBooks(ctx context.Context, p principal, id string) (events.APIGatewayV2HTTPResponse, error) {
	if _, ok := a.canAccess(ctx, p, id); !ok {
		return response(403, map[string]string{"error": "no library access"}), nil
	}
	items, err := a.queryPrefix(ctx, "LIBRARY#"+id, "BOOK#")
	if err != nil {
		return response(500, nil), err
	}
	out := []Book{}
	for _, it := range items {
		var r storedBook
		if attributevalue.UnmarshalMap(it, &r) != nil || !isCurrentBook(r.Book) {
			continue
		}
		if r.RegisteredAt == "" {
			r.RegisteredAt = r.CreatedAt
		}
		r.Reaction = normalizeReaction(r.Reaction)
		out = append(out, r.Book)
	}
	return response(200, map[string]any{"items": out}), nil
}
func (a *app) resolveAuthor(ctx context.Context, userID, id, name string) (Author, error) {
	if id != "" {
		var x struct {
			PK, SK string
			Author `dynamodbav:",inline"`
		}
		if ok, e := a.get(ctx, "USER#"+userID, "AUTHOR#"+id, &x); ok && strings.TrimSpace(x.Name) != "" {
			return x.Author, e
		}
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Author{}, nil
	}
	items, e := a.queryPrefix(ctx, "USER#"+userID, "AUTHOR#")
	if e != nil {
		return Author{}, e
	}
	for _, it := range items {
		var x struct {
			Author `dynamodbav:",inline"`
		}
		_ = attributevalue.UnmarshalMap(it, &x)
		if strings.EqualFold(x.Name, name) {
			return x.Author, nil
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	x := Author{ID: uuid.NewString(), Name: name, CreatedAt: now, UpdatedAt: now}
	e = a.put(ctx, struct {
		PK, SK string
		Author `dynamodbav:",inline"`
	}{"USER#" + userID, "AUTHOR#" + x.ID, x})
	return x, e
}
func (a *app) resolveCategory(ctx context.Context, userID, id, name string) (Category, error) {
	if id != "" {
		var x struct {
			PK, SK   string
			Category `dynamodbav:",inline"`
		}
		if ok, e := a.get(ctx, "USER#"+userID, "CATEGORY#"+id, &x); ok {
			return x.Category, e
		}
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Category{}, nil
	}
	items, e := a.queryPrefix(ctx, "USER#"+userID, "CATEGORY#")
	if e != nil {
		return Category{}, e
	}
	for _, it := range items {
		var x struct {
			Category `dynamodbav:",inline"`
		}
		_ = attributevalue.UnmarshalMap(it, &x)
		if strings.EqualFold(x.Name, name) {
			return x.Category, nil
		}
	}
	x := Category{ID: uuid.NewString(), Name: name}
	e = a.put(ctx, struct {
		PK, SK   string
		Category `dynamodbav:",inline"`
	}{"USER#" + userID, "CATEGORY#" + x.ID, x})
	return x, e
}
func (a *app) createLibraryBook(ctx context.Context, p principal, libID, body string) (events.APIGatewayV2HTTPResponse, error) {
	lib, ok := a.canAccess(ctx, p, libID)
	if !ok {
		return response(403, map[string]string{"error": "no library access"}), nil
	}
	if lib.OwnerID != p.ID {
		return response(http.StatusForbidden, map[string]string{"error": "shared libraries are read-only"}), nil
	}
	var in createBookRequest
	if json.Unmarshal([]byte(body), &in) != nil || strings.TrimSpace(in.Title) == "" {
		return response(400, map[string]string{"error": "title is required"}), nil
	}
	var au Author
	var err error
	if strings.TrimSpace(in.AuthorID) != "" || strings.TrimSpace(in.Author) != "" {
		au, err = a.resolveAuthor(ctx, p.ID, in.AuthorID, in.Author)
		if err != nil {
			return response(500, nil), err
		}
	}
	var cat Category
	if strings.TrimSpace(in.CategoryID) != "" || strings.TrimSpace(in.Category) != "" {
		cat, err = a.resolveCategory(ctx, p.ID, in.CategoryID, in.Category)
		if err != nil {
			return response(500, nil), err
		}
	}
	st := in.Status
	if st == "" {
		st = "owned"
	}
	if !validStatus(st) {
		return response(400, map[string]string{"error": "invalid status"}), nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	reaction := normalizeReaction(in.Reaction)
	b := Book{ID: uuid.NewString(), Title: strings.TrimSpace(in.Title), AuthorID: au.ID, Author: au.Name, ISBN: strings.TrimSpace(in.ISBN), CategoryID: cat.ID, Category: cat.Name, Status: st, Notes: strings.TrimSpace(in.Notes), RegisteredAt: now, BoughtAt: normalizeDate(in.BoughtAt), ReadAt: normalizeDate(in.ReadAt), PublishedAt: normalizeDate(in.PublishedAt), Language: normalizeBookLanguage(in.Language), Reaction: reaction, CreatedByID: p.ID, CreatedAt: now, UpdatedAt: now}
	err = a.put(ctx, storedBook{PK: "LIBRARY#" + libID, SK: "BOOK#" + b.ID, Book: b})
	return response(201, b), err
}
func isCurrentBook(b Book) bool {
	return strings.TrimSpace(b.ID) != "" && strings.TrimSpace(b.Title) != "" && strings.TrimSpace(b.CreatedByID) != ""
}

func (a *app) moveLibraryBook(ctx context.Context, p principal, sourceLibID, bookID, body string) (events.APIGatewayV2HTTPResponse, error) {
	sourceLib, ok := a.canAccess(ctx, p, sourceLibID)
	if !ok {
		return response(http.StatusForbidden, map[string]string{"error": "no source library access"}), nil
	}
	if sourceLib.OwnerID != p.ID {
		return response(http.StatusForbidden, map[string]string{"error": "shared libraries are read-only"}), nil
	}
	var in struct {
		TargetLibraryID string `json:"targetLibraryId"`
	}
	if err := json.Unmarshal([]byte(body), &in); err != nil || strings.TrimSpace(in.TargetLibraryID) == "" {
		return response(http.StatusBadRequest, map[string]string{"error": "targetLibraryId is required"}), nil
	}
	targetLibID := strings.TrimSpace(in.TargetLibraryID)
	if targetLibID == sourceLibID {
		return response(http.StatusBadRequest, map[string]string{"error": "target library must be different from source library"}), nil
	}
	targetLib, ok := a.canAccess(ctx, p, targetLibID)
	if !ok {
		return response(http.StatusForbidden, map[string]string{"error": "no target library access"}), nil
	}
	if targetLib.OwnerID != p.ID {
		return response(http.StatusForbidden, map[string]string{"error": "shared libraries are read-only"}), nil
	}

	var source storedBook
	ok, err := a.get(ctx, "LIBRARY#"+sourceLibID, "BOOK#"+bookID, &source)
	if err != nil {
		return response(http.StatusInternalServerError, map[string]string{"error": "failed to get book"}), err
	}
	if !ok || !isCurrentBook(source.Book) {
		return response(http.StatusNotFound, map[string]string{"error": "book not found"}), nil
	}

	// Keep the exact book payload, changing only its DynamoDB location.
	destination := storedBook{PK: "LIBRARY#" + targetLibID, SK: "BOOK#" + bookID, Book: source.Book}
	destination.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)

	// AWS supports the transactional move. For Kumo/local mode use a verified
	// copy/delete sequence because this emulation does not provide the same
	// transaction guarantees for every DynamoDB operation.
	if a.authMode != authModeLocal {
		item, err := attributevalue.MarshalMap(destination)
		if err != nil {
			return response(http.StatusInternalServerError, map[string]string{"error": "failed to encode book"}), err
		}
		_, err = a.db.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
			TransactItems: []types.TransactWriteItem{
				{Put: &types.Put{
					TableName:           aws.String(a.tableName),
					Item:                item,
					ConditionExpression: aws.String("attribute_not_exists(PK) AND attribute_not_exists(SK)"),
				}},
				{Delete: &types.Delete{
					TableName: aws.String(a.tableName),
					Key: map[string]types.AttributeValue{
						"PK": &types.AttributeValueMemberS{Value: source.PK},
						"SK": &types.AttributeValueMemberS{Value: source.SK},
					},
					ConditionExpression: aws.String("attribute_exists(PK) AND attribute_exists(SK)"),
				}},
			},
		})
		if err != nil {
			return response(http.StatusConflict, map[string]string{"error": "book could not be moved; no changes were made"}), err
		}
		return response(http.StatusOK, destination.Book), nil
	}

	// Local/Kumo: verify every step and roll back the destination if deleting
	// the source fails. This avoids a silent partial move in the emulator.
	var existing storedBook
	if exists, err := a.get(ctx, destination.PK, destination.SK, &existing); err != nil {
		return response(http.StatusInternalServerError, map[string]string{"error": "failed to check destination book"}), err
	} else if exists {
		return response(http.StatusConflict, map[string]string{"error": "a book with this id already exists in the target library"}), nil
	}
	if err := a.put(ctx, destination); err != nil {
		return response(http.StatusInternalServerError, map[string]string{"error": "failed to create destination book"}), err
	}
	var verify storedBook
	created, err := a.get(ctx, destination.PK, destination.SK, &verify)
	if err != nil || !created || !isCurrentBook(verify.Book) {
		_ = a.deleteRawBook(ctx, destination.PK, destination.SK)
		if err != nil {
			return response(http.StatusInternalServerError, map[string]string{"error": "failed to verify destination book"}), err
		}
		return response(http.StatusConflict, map[string]string{"error": "destination book could not be verified"}), nil
	}

	if err := a.deleteRawBook(ctx, source.PK, source.SK); err != nil {
		_ = a.deleteRawBook(ctx, destination.PK, destination.SK)
		return response(http.StatusConflict, map[string]string{"error": "book could not be moved; no changes were made"}), err
	}

	var removed storedBook
	stillThere, err := a.get(ctx, source.PK, source.SK, &removed)
	if err != nil {
		// Restore the source so the caller never loses the book because of a
		// verification failure.
		_ = a.put(ctx, source)
		_ = a.deleteRawBook(ctx, destination.PK, destination.SK)
		return response(http.StatusInternalServerError, map[string]string{"error": "failed to verify source deletion; move rolled back"}), err
	}
	if stillThere {
		_ = a.put(ctx, source)
		_ = a.deleteRawBook(ctx, destination.PK, destination.SK)
		return response(http.StatusConflict, map[string]string{"error": "source book could not be deleted; move rolled back"}), nil
	}

	return response(http.StatusOK, destination.Book), nil
}

func (a *app) deleteRawBook(ctx context.Context, pk, sk string) error {
	_, err := a.db.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(a.tableName),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: pk},
			"SK": &types.AttributeValueMemberS{Value: sk},
		},
	})
	return err
}

func (a *app) updateLibraryBook(ctx context.Context, p principal, libID, bookID, body string) (events.APIGatewayV2HTTPResponse, error) {
	lib, ok := a.canAccess(ctx, p, libID)
	if !ok {
		return response(403, map[string]string{"error": "no library access"}), nil
	}
	if lib.OwnerID != p.ID {
		return response(http.StatusForbidden, map[string]string{"error": "shared libraries are read-only"}), nil
	}
	var row storedBook
	if ok, e := a.get(ctx, "LIBRARY#"+libID, "BOOK#"+bookID, &row); e != nil || !ok {
		return response(404, nil), e
	}
	var in updateBookRequest
	if json.Unmarshal([]byte(body), &in) != nil {
		return response(400, nil), nil
	}
	if in.Title != nil {
		row.Title = strings.TrimSpace(*in.Title)
	}
	aid, an := row.AuthorID, row.Author
	if in.AuthorID != nil {
		aid = *in.AuthorID
	}
	if in.Author != nil {
		an = *in.Author
	}
	var au Author
	if strings.TrimSpace(aid) != "" || strings.TrimSpace(an) != "" {
		resolved, err := a.resolveAuthor(ctx, p.ID, aid, an)
		if err != nil {
			return response(500, nil), err
		}
		au = resolved
		row.AuthorID, row.Author = au.ID, au.Name
	} else {
		row.AuthorID, row.Author = "", ""
	}
	cid, cn := row.CategoryID, row.Category
	if in.CategoryID != nil {
		cid = *in.CategoryID
	}
	if in.Category != nil {
		cn = *in.Category
	}
	if strings.TrimSpace(cid) != "" || strings.TrimSpace(cn) != "" {
		cat, e := a.resolveCategory(ctx, p.ID, cid, cn)
		if e != nil {
			return response(500, nil), e
		}
		row.CategoryID, row.Category = cat.ID, cat.Name
	} else {
		row.CategoryID, row.Category = "", ""
	}
	if in.ISBN != nil {
		row.ISBN = strings.TrimSpace(*in.ISBN)
	}
	if in.Status != nil {
		row.Status = *in.Status
	}
	if in.Notes != nil {
		row.Notes = strings.TrimSpace(*in.Notes)
	}
	if in.BoughtAt != nil {
		row.BoughtAt = normalizeDate(*in.BoughtAt)
	}
	if in.ReadAt != nil {
		row.ReadAt = normalizeDate(*in.ReadAt)
	}
	if in.PublishedAt != nil {
		row.PublishedAt = normalizeDate(*in.PublishedAt)
	}
	if in.Language != nil {
		row.Language = normalizeBookLanguage(*in.Language)
	}
	if in.Reaction != nil {
		row.Reaction = normalizeReaction(*in.Reaction)
	}
	row.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if row.RegisteredAt == "" {
		row.RegisteredAt = row.CreatedAt
	}
	err := a.put(ctx, row)
	return response(200, row.Book), err
}
func (a *app) deleteLibraryBook(ctx context.Context, p principal, libID, bookID string) (events.APIGatewayV2HTTPResponse, error) {
	lib, ok := a.canAccess(ctx, p, libID)
	if !ok {
		return response(403, map[string]string{"error": "no library access"}), nil
	}
	if lib.OwnerID != p.ID {
		return response(http.StatusForbidden, map[string]string{"error": "shared libraries are read-only"}), nil
	}
	out, err := a.db.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(a.tableName),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: "LIBRARY#" + libID},
			"SK": &types.AttributeValueMemberS{Value: "BOOK#" + bookID},
		},
		ReturnValues: types.ReturnValueAllOld,
	})
	if err != nil {
		return response(http.StatusInternalServerError, map[string]string{"error": "failed to delete book"}), err
	}
	if len(out.Attributes) == 0 {
		return response(http.StatusNotFound, map[string]string{"error": "book not found"}), nil
	}
	if err := a.deleteBookComments(ctx, bookID); err != nil {
		return response(http.StatusInternalServerError, map[string]string{"error": "book deleted but comments could not be cleaned up"}), err
	}
	return response(http.StatusNoContent, nil), nil
}

func (a *app) deleteBookComments(ctx context.Context, bookID string) error {
	items, err := a.queryPrefix(ctx, commentPartitionKey(bookID), "COMMENT#")
	if err != nil {
		return err
	}
	for _, it := range items {
		pk, okPK := it["PK"].(*types.AttributeValueMemberS)
		sk, okSK := it["SK"].(*types.AttributeValueMemberS)
		if !okPK || !okSK {
			continue
		}
		if err := a.deleteItemByKey(ctx, pk.Value, sk.Value); err != nil {
			return err
		}
	}
	return nil
}

func (a *app) listAuthors(ctx context.Context, userID string) (events.APIGatewayV2HTTPResponse, error) {
	items, e := a.queryPrefix(ctx, "USER#"+userID, "AUTHOR#")
	out := []Author{}
	seen := map[string]bool{}
	for _, it := range items {
		if sk, ok := it["SK"].(*types.AttributeValueMemberS); !ok || !strings.HasPrefix(sk.Value, "AUTHOR#") {
			continue
		}
		var x struct {
			Author `dynamodbav:",inline"`
		}
		if attributevalue.UnmarshalMap(it, &x) != nil {
			continue
		}
		x.Name = strings.TrimSpace(x.Name)
		if x.Name == "" || seen[strings.ToLower(x.Name)] {
			continue
		}
		seen[strings.ToLower(x.Name)] = true
		out = append(out, x.Author)
	}
	return response(200, map[string]any{"items": out}), e
}
func (a *app) createAuthor(ctx context.Context, userID, body string) (events.APIGatewayV2HTTPResponse, error) {
	var in Author
	if json.Unmarshal([]byte(body), &in) != nil || strings.TrimSpace(in.Name) == "" {
		return response(400, map[string]string{"error": "name required"}), nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	in.ID = uuid.NewString()
	in.Name = strings.TrimSpace(in.Name)
	in.CreatedAt = now
	in.UpdatedAt = now
	e := a.put(ctx, struct {
		PK, SK string
		Author `dynamodbav:",inline"`
	}{"USER#" + userID, "AUTHOR#" + in.ID, in})
	return response(201, in), e
}
func (a *app) getAuthor(ctx context.Context, userID, id string) (events.APIGatewayV2HTTPResponse, error) {
	var x struct {
		PK, SK string
		Author `dynamodbav:",inline"`
	}
	ok, e := a.get(ctx, "USER#"+userID, "AUTHOR#"+id, &x)
	if !ok {
		return response(404, nil), e
	}
	return response(200, x.Author), e
}
func (a *app) updateAuthor(ctx context.Context, userID, id, body string) (events.APIGatewayV2HTTPResponse, error) {
	var x struct {
		PK, SK string
		Author `dynamodbav:",inline"`
	}
	ok, e := a.get(ctx, "USER#"+userID, "AUTHOR#"+id, &x)
	if !ok {
		return response(404, nil), e
	}
	var in Author
	if json.Unmarshal([]byte(body), &in) != nil {
		return response(400, nil), nil
	}
	if strings.TrimSpace(in.Name) != "" {
		x.Name = strings.TrimSpace(in.Name)
	}
	x.Country = strings.TrimSpace(in.Country)
	x.BirthDate = strings.TrimSpace(in.BirthDate)
	x.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	e = a.put(ctx, x)
	return response(200, x.Author), e
}
func (a *app) listCategories(ctx context.Context, userID string) (events.APIGatewayV2HTTPResponse, error) {
	items, e := a.queryPrefix(ctx, "USER#"+userID, "CATEGORY#")
	out := []Category{}
	seen := map[string]bool{}
	for _, it := range items {
		if sk, ok := it["SK"].(*types.AttributeValueMemberS); !ok || !strings.HasPrefix(sk.Value, "CATEGORY#") {
			continue
		}
		var x struct {
			Category `dynamodbav:",inline"`
		}
		if attributevalue.UnmarshalMap(it, &x) != nil {
			continue
		}
		x.Name = strings.TrimSpace(x.Name)
		if x.Name == "" || seen[strings.ToLower(x.Name)] {
			continue
		}
		seen[strings.ToLower(x.Name)] = true
		out = append(out, x.Category)
	}
	return response(200, map[string]any{"items": out}), e
}
func (a *app) createCategory(ctx context.Context, userID, body string) (events.APIGatewayV2HTTPResponse, error) {
	var in Category
	if json.Unmarshal([]byte(body), &in) != nil || strings.TrimSpace(in.Name) == "" {
		return response(400, nil), nil
	}
	x, e := a.resolveCategory(ctx, userID, "", in.Name)
	return response(201, x), e
}
func (a *app) getCategory(ctx context.Context, userID, id string) (events.APIGatewayV2HTTPResponse, error) {
	var x struct {
		PK, SK   string
		Category `dynamodbav:",inline"`
	}
	ok, e := a.get(ctx, "USER#"+userID, "CATEGORY#"+id, &x)
	if !ok {
		return response(404, nil), e
	}
	return response(200, x.Category), e
}

func (a *app) updateCategory(ctx context.Context, userID, id, body string) (events.APIGatewayV2HTTPResponse, error) {
	var x struct {
		PK, SK   string
		Category `dynamodbav:",inline"`
	}
	ok, e := a.get(ctx, "USER#"+userID, "CATEGORY#"+id, &x)
	if !ok {
		return response(404, nil), e
	}
	var in Category
	if json.Unmarshal([]byte(body), &in) != nil || strings.TrimSpace(in.Name) == "" {
		return response(400, map[string]string{"error": "name required"}), nil
	}
	x.Name = strings.TrimSpace(in.Name)
	e = a.put(ctx, x)
	return response(200, x.Category), e
}

func (a *app) getProfile(ctx context.Context, p principal) (events.APIGatewayV2HTTPResponse, error) {
	var row struct{ PK, SK, UserID, Email, Name, BirthDate, Country, PreferredLanguage string }
	ok, e := a.get(ctx, "USER#"+p.ID, "PROFILE", &row)
	if !ok && a.authMode == authModeLocal && p.Email != "" {
		// Backward compatibility for local users created before the USER#<id> profile row.
		var legacy localUser
		if legacyOK, legacyErr := a.get(ctx, "EMAIL#"+p.Email, "PROFILE", &legacy); legacyErr == nil && legacyOK {
			row.UserID, row.Email, row.Name, row.BirthDate, row.Country, row.PreferredLanguage = legacy.UserID, legacy.Email, legacy.Name, legacy.BirthDate, legacy.Country, legacy.PreferredLanguage
			if p.ID == row.UserID {
				return response(200, Profile{ID: p.ID, Email: p.Email, Name: row.Name, BirthDate: row.BirthDate, Country: row.Country, PreferredLanguage: normalizePreferredLanguage(row.PreferredLanguage)}), nil
			}
		}
	}
	if !ok {
		return response(200, Profile{ID: p.ID, Email: p.Email}), e
	}
	return response(200, Profile{ID: p.ID, Email: p.Email, Name: row.Name, BirthDate: row.BirthDate, Country: row.Country, PreferredLanguage: normalizePreferredLanguage(row.PreferredLanguage)}), e
}
func (a *app) updateProfile(ctx context.Context, p principal, body string) (events.APIGatewayV2HTTPResponse, error) {
	var in Profile
	if json.Unmarshal([]byte(body), &in) != nil {
		return response(400, nil), nil
	}
	name, birthDate, country := strings.TrimSpace(in.Name), strings.TrimSpace(in.BirthDate), strings.TrimSpace(in.Country)
	preferredLanguage := normalizePreferredLanguage(in.PreferredLanguage)
	row := struct{ PK, SK, UserID, Email, Name, BirthDate, Country, PreferredLanguage string }{"USER#" + p.ID, "PROFILE", p.ID, p.Email, name, birthDate, country, preferredLanguage}
	if err := a.put(ctx, row); err != nil {
		return response(500, map[string]string{"error": "failed to save personal data"}), err
	}
	if a.authMode == authModeLocal && p.Email != "" {
		var u localUser
		if ok, _ := a.get(ctx, "EMAIL#"+p.Email, "PROFILE", &u); ok {
			u.Name, u.BirthDate, u.Country, u.PreferredLanguage = name, birthDate, country, preferredLanguage
			if err := a.put(ctx, u); err != nil {
				return response(500, map[string]string{"error": "failed to save account profile"}), err
			}
		}
	}
	return response(200, Profile{ID: p.ID, Email: p.Email, Name: name, BirthDate: birthDate, Country: country, PreferredLanguage: preferredLanguage}), nil
}
func (a *app) changeLocalPassword(ctx context.Context, p principal, body string) (events.APIGatewayV2HTTPResponse, error) {
	if a.authMode != authModeLocal {
		return response(501, map[string]string{"error": "In AWS mode use Cognito password recovery/update from the sign-in service."}), nil
	}
	var in struct{ CurrentPassword, NewPassword string }
	if json.Unmarshal([]byte(body), &in) != nil || len(in.NewPassword) < 8 {
		return response(400, map[string]string{"error": "new password must have at least 8 characters"}), nil
	}
	var u localUser
	ok, e := a.get(ctx, "EMAIL#"+p.Email, "PROFILE", &u)
	if e != nil || !ok {
		return response(404, nil), e
	}
	if !verifyPassword(in.CurrentPassword, u.PasswordHash) {
		return response(400, map[string]string{"error": "current password is incorrect"}), nil
	}
	u.PasswordHash = hashPassword(in.NewPassword)
	e = a.put(ctx, u)
	return response(204, nil), e
}

func normalizeBookLanguage(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "en", "es", "pt", "fr", "de":
		return v
	default:
		return ""
	}
}

func normalizeReaction(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "like":
		return "like"
	case "dislike":
		return "dislike"
	default:
		return ""
	}
}

func normalizeDate(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if _, err := time.Parse("2006-01-02", v); err != nil {
		return ""
	}
	return v
}

func validStatus(status string) bool {
	switch status {
	case "owned", "reading", "read", "wishlist", "lent", "sold", "donated":
		return true
	default:
		return false
	}
}

func claimsSub(req events.APIGatewayV2HTTPRequest) string {
	return req.RequestContext.Authorizer.JWT.Claims["sub"]
}
func claimsEmail(req events.APIGatewayV2HTTPRequest) string {
	return normalizeEmail(req.RequestContext.Authorizer.JWT.Claims["email"])
}

func splitPath(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	parts := strings.Split(path, "/")
	clean := parts[:0]
	for _, part := range parts {
		if part != "" {
			clean = append(clean, part)
		}
	}
	return clean
}

func corsResponse(status int, body any) events.APIGatewayV2HTTPResponse {
	resp := response(status, body)
	if resp.Headers == nil {
		resp.Headers = map[string]string{}
	}
	resp.Headers["Access-Control-Allow-Origin"] = "*"
	resp.Headers["Access-Control-Allow-Headers"] = "authorization,content-type"
	resp.Headers["Access-Control-Allow-Methods"] = "DELETE,GET,OPTIONS,PATCH,POST"
	resp.Headers["Access-Control-Max-Age"] = "300"
	return resp
}

func response(status int, payload any) events.APIGatewayV2HTTPResponse {
	body := ""
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			body = `{"error":"serialization failure"}`
		} else {
			body = string(b)
		}
	}
	return events.APIGatewayV2HTTPResponse{
		StatusCode: status,
		Headers: map[string]string{
			"content-type":  "application/json",
			"cache-control": "no-store",
		},
		Body: body,
	}
}
