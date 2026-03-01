package main

import (
	"database/sql"
	"embed"
	"encoding/csv"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"gopkg.in/yaml.v3"
)

//go:embed layout.html welcome.html form.html thanks.html list.html config_manager.html login.html register.html
var templateFS embed.FS

const (
	dbFile = "database.db"
)

var db *sql.DB

func initDB() error {
	var err error
	db, err = sql.Open("sqlite3", dbFile)
	if err != nil {
		return err
	}

	if err = db.Ping(); err != nil {
		return err
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS responses (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			config_file TEXT NOT NULL,
			username TEXT NOT NULL,
			created_at TEXT NOT NULL,
			fields TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_config ON responses(config_file);
		CREATE INDEX IF NOT EXISTS idx_username ON responses(username);

		CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL UNIQUE,
			password TEXT NOT NULL,
			is_admin INTEGER NOT NULL DEFAULT 0
		);
		CREATE INDEX IF NOT EXISTS idx_user_username ON users(username);
	`)
	if err != nil {
		return err
	}

	fmt.Println("数据库初始化成功")
	return nil
}

func getResponsesCSVFile(configFile string) string {
	name := strings.TrimSuffix(configFile, ".yaml")
	name = strings.TrimSuffix(name, ".yml")
	return name + "_responses.csv"
}

type User struct {
	Username string
	Password string
	IsAdmin  bool
}

type Rsvp struct {
	Fields     map[string]string `json:"-"`
	Config     FormConfig        `json:"-"`
	ConfigFile string            `json:"-"`
	Username   string            `json:"-"`
	CreatedAt  time.Time         `json:"-"`
}

func (r *Rsvp) GetField(name string) string {
	if r.Fields == nil {
		return ""
	}
	return r.Fields[name]
}

type FormField struct {
	Name        string       `yaml:"name" json:"name"`
	Label       string       `yaml:"label" json:"label"`
	Type        string       `yaml:"type" json:"type"`
	Required    bool         `yaml:"required" json:"required"`
	Placeholder string       `yaml:"placeholder,omitempty" json:"placeholder,omitempty"`
	Min         string       `yaml:"min,omitempty" json:"min,omitempty"`
	Max         string       `yaml:"max,omitempty" json:"max,omitempty"`
	Step        string       `yaml:"step,omitempty" json:"step,omitempty"`
	Options     []FormOption `yaml:"options,omitempty" json:"options,omitempty"`
	Value       string       `yaml:"value,omitempty" json:"value,omitempty"`
}

type FormOption struct {
	Value string `yaml:"value" json:"value"`
	Text  string `yaml:"text" json:"text"`
}

type FormConfig struct {
	Title   string       `yaml:"title" json:"title"`
	Fields  []FormField  `yaml:"fields" json:"fields"`
	Buttons []FormButton `yaml:"buttons" json:"buttons"`
}

type FormButton struct {
	Type  string `yaml:"type" json:"type"`
	Text  string `yaml:"text" json:"text"`
	Class string `yaml:"class,omitempty" json:"class,omitempty"`
	Href  string `yaml:"href,omitempty" json:"href,omitempty"`
}

var (
	responses      = make([]*Rsvp, 0, 10)
	responsesMutex sync.RWMutex

	templates      = make(map[string]*template.Template, 3)
	templatesMutex sync.RWMutex

	formConfig      FormConfig
	formConfigMutex sync.RWMutex

	currentConfigFile = "form_form.yaml"

	users      = make(map[string]*User)
	usersMutex sync.RWMutex

	sessions      = make(map[string]*User)
	sessionsMutex sync.RWMutex
)

func getAvailableConfigs() []string {
	files, err := os.ReadDir(".")
	if err != nil {
		return []string{currentConfigFile}
	}

	var configs []string
	for _, file := range files {
		if strings.HasSuffix(file.Name(), "_form.yaml") ||
			strings.HasSuffix(file.Name(), "_form.yml") ||
			file.Name() == "form_config.yaml" {
			configs = append(configs, file.Name())
		}
	}

	if len(configs) == 0 {
		configs = append(configs, currentConfigFile)
	}

	return configs
}

func loadFormConfig() error {
	return loadFormConfigFromFile(currentConfigFile)
}

func loadFormConfigFromFile(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("无法读取配置文件 %s: %v", filename, err)
	}

	formConfigMutex.Lock()
	defer formConfigMutex.Unlock()

	err = yaml.Unmarshal(data, &formConfig)
	if err != nil {
		return fmt.Errorf("解析配置文件 %s 失败: %v", filename, err)
	}

	currentConfigFile = filename
	fmt.Printf("成功加载表单配置: %s (来自 %s)\n", formConfig.Title, filename)
	return nil
}

// loadFormConfigForList 加载指定配置文件的表单配置，不影响全局配置
func loadFormConfigForList(filename string) (FormConfig, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return FormConfig{}, fmt.Errorf("无法读取配置文件 %s: %v", filename, err)
	}

	var config FormConfig
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return FormConfig{}, fmt.Errorf("解析配置文件 %s 失败: %v", filename, err)
	}

	return config, nil
}

func initUsers() {
	loadUsersFromDB()
}

func loadUsersFromDB() error {
	usersMutex.Lock()
	defer usersMutex.Unlock()

	rows, err := db.Query("SELECT username, password, is_admin FROM users")
	if err != nil {
		var adminExists bool
		err2 := db.QueryRow("SELECT 1 FROM users WHERE username = 'admin'").Scan(&adminExists)
		if err2 != nil && err2 == sql.ErrNoRows {
			_, err = db.Exec("INSERT INTO users (username, password, is_admin) VALUES (?, ?, ?)",
				"admin", "admin", 1)
			if err != nil {
				return err
			}
			users["admin"] = &User{
				Username: "admin",
				Password: "admin",
				IsAdmin:  true,
			}
			fmt.Println("创建默认管理员账号: admin/admin")
		}
		return nil
	}
	defer rows.Close()

	for rows.Next() {
		var username, password string
		var isAdmin int
		if err := rows.Scan(&username, &password, &isAdmin); err != nil {
			continue
		}
		users[username] = &User{
			Username: username,
			Password: password,
			IsAdmin:  isAdmin == 1,
		}
	}

	if _, exists := users["admin"]; !exists {
		_, err = db.Exec("INSERT INTO users (username, password, is_admin) VALUES (?, ?, ?)",
			"admin", "admin", 1)
		if err == nil {
			users["admin"] = &User{
				Username: "admin",
				Password: "admin",
				IsAdmin:  true,
			}
		}
	}

	fmt.Printf("成功加载 %d 个用户 from 数据库\n", len(users))
	return nil
}

func saveUserToDB(user *User) error {
	_, err := db.Exec(
		"INSERT OR REPLACE INTO users (username, password, is_admin) VALUES (?, ?, ?)",
		user.Username, user.Password, boolToInt(user.IsAdmin),
	)
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func initResponsesFromCSV() error {
	responsesMutex.Lock()
	defer responsesMutex.Unlock()

	rows, err := db.Query("SELECT config_file, username, created_at, fields FROM responses ORDER BY created_at DESC")
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var configFile, username, createdAtStr, fieldsJSON string
		if err := rows.Scan(&configFile, &username, &createdAtStr, &fieldsJSON); err != nil {
			continue
		}

		rsvp := &Rsvp{
			Fields:     make(map[string]string),
			ConfigFile: configFile,
			Username:   username,
		}

		if createdAt, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
			rsvp.CreatedAt = createdAt
		}

		var fields map[string]string
		if err := yaml.Unmarshal([]byte(fieldsJSON), &fields); err == nil {
			rsvp.Fields = fields
		}

		responses = append(responses, rsvp)
	}

	if len(responses) > 0 {
		fmt.Printf("成功加载 %d 条响应记录 from 数据库\n", len(responses))
	}
	return nil
}

func saveResponseToDB(rsvp *Rsvp) error {
	fieldsJSON, err := yaml.Marshal(rsvp.Fields)
	if err != nil {
		return err
	}

	_, err = db.Exec(
		"INSERT INTO responses (config_file, username, created_at, fields) VALUES (?, ?, ?, ?)",
		rsvp.ConfigFile,
		rsvp.Username,
		rsvp.CreatedAt.Format(time.RFC3339),
		string(fieldsJSON),
	)

	return err
}

func createSession(username string) string {
	sessionID := fmt.Sprintf("%d", time.Now().UnixNano())

	usersMutex.RLock()
	user := users[username]
	usersMutex.RUnlock()

	sessionsMutex.Lock()
	sessions[sessionID] = user
	sessionsMutex.Unlock()

	return sessionID
}

func getUserFromSession(sessionID string) *User {
	if sessionID == "" {
		return nil
	}

	sessionsMutex.RLock()
	user := sessions[sessionID]
	sessionsMutex.RUnlock()

	return user
}

func getCurrentUser(c *gin.Context) *User {
	sessionID, err := c.Cookie("session_id")
	if err != nil {
		return nil
	}
	return getUserFromSession(sessionID)
}

func authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		user := getCurrentUser(c)
		if user == nil {
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}
		c.Set("user", user)
		c.Next()
	}
}

// 切换表单配置处理器
func switchConfigHandler(c *gin.Context) {
	if c.Request.Method == http.MethodPost {
		c.Request.ParseForm()
		configFile := c.Request.FormValue("config")
		if configFile != "" {
			if err := loadFormConfigFromFile(configFile); err != nil {
				c.String(400, err.Error())
				return
			}
		}
	}

	c.Redirect(http.StatusSeeOther, "/config-manager")
}

func loadTemplates() {
	templateNames := [5]string{"welcome", "form", "thanks", "list", "config_manager"}
	for index, name := range templateNames {
		t, err := template.New("layout.html").Funcs(template.FuncMap{
			"add": func(a, b int) int { return a + b },
			"sub": func(a, b int) int { return a - b },
			"div": func(a, b int) float64 {
				if b == 0 {
					return 0
				}
				return float64(a) / float64(b)
			},
			"mul": func(a, b float64) float64 { return a * b },
			"GetFieldValue": func(fd formData, fieldName string) string {
				for _, field := range fd.FormConfig.Fields {
					if field.Name == fieldName {
						return field.Value
					}
				}
				return ""
			},
			"getMapValue": func(m map[string]string, key string) string {
				if m == nil {
					return ""
				}
				return m[key]
			},
			"split": func(s, sep string) []string {
				return strings.Split(s, sep)
			},
		}).ParseFS(templateFS, "layout.html", name+".html")
		if err == nil {
			templates[name] = t
			fmt.Printf("成功加载模板 %d: %s\n", index, name)
		} else {
			fmt.Printf("加载模板 %s 失败: %v\n", name, err)
		}
	}

	loginT, err := template.ParseFS(templateFS, "login.html")
	if err == nil {
		templates["login"] = loginT
		fmt.Println("成功加载模板: login")
	} else {
		fmt.Printf("加载模板 login 失败: %v\n", err)
	}

	registerT, err := template.ParseFS(templateFS, "register.html")
	if err == nil {
		templates["register"] = registerT
		fmt.Println("成功加载模板: register")
	} else {
		fmt.Printf("加载模板 register 失败: %v\n", err)
	}

	requiredTemplates := []string{"welcome", "form", "thanks"}
	for _, name := range requiredTemplates {
		if templates[name] == nil {
			fmt.Printf("警告: 必需模板 %s 未加载\n", name)
		}
	}
}

func welcomeHandler(c *gin.Context) {
	fmt.Println("处理主页请求...")

	err := templates["welcome"].Execute(c.Writer, nil)
	if err != nil {
		fmt.Printf("模板执行错误: %v\n", err)
		c.String(500, "模板执行失败: "+err.Error())
		return
	}
}

type listData struct {
	Responses           []*Rsvp
	FormConfig          FormConfig
	TotalCount          int
	ResponsesWithConfig []*RsvpWithConfig
	CurrentUser         *User
	AvailableConfigs    []string
	SelectedConfig      string
	AllResponses        []*RsvpWithConfig
}

// RsvpWithConfig 用于在列表中配对响应和其配置
type RsvpWithConfig struct {
	Response *Rsvp
	Config   FormConfig
}

func listHandler(c *gin.Context) {
	user := getCurrentUser(c)
	if user == nil {
		c.Redirect(http.StatusFound, "/login")
		return
	}

	// 获取用户选择的配置筛选参数
	selectedConfig := c.Query("config")
	if selectedConfig == "" {
		selectedConfig = "all" // 默认显示所有配置
	}

	responsesMutex.RLock()
	defer responsesMutex.RUnlock()

	formConfigMutex.RLock()
	defer formConfigMutex.RUnlock()

	// 获取所有可用的配置列表
	availableConfigs := getAvailableConfigs()

	// 首先筛选出用户有权限查看的所有响应（不按配置筛选）
	userResponses := make([]*Rsvp, 0)
	for _, response := range responses {
		if user.IsAdmin || response.Username == user.Username {
			userResponses = append(userResponses, response)
		}
	}

	// 再按选择的配置进行筛选
	filteredResponses := make([]*Rsvp, 0)
	for _, response := range userResponses {
		if selectedConfig == "all" || response.ConfigFile == selectedConfig || response.ConfigFile == "" {
			filteredResponses = append(filteredResponses, response)
		}
	}

	// 加载所有配置信息用于显示
	configCache := make(map[string]FormConfig)
	configCache[currentConfigFile] = formConfig

	// 预加载所有可用配置到缓存
	for _, configFile := range availableConfigs {
		if _, ok := configCache[configFile]; !ok {
			if cfg, err := loadFormConfigForList(configFile); err == nil {
				configCache[configFile] = cfg
			}
		}
	}

	// 确定表格抬头使用的配置
	// 如果选择了特定配置，使用该配置的字段；否则使用当前全局配置
	displayFormConfig := formConfig
	if selectedConfig != "all" {
		if cfg, ok := configCache[selectedConfig]; ok {
			displayFormConfig = cfg
		}
	}

	// 构建所有用户有权限查看的响应（用于显示配置来源）
	allResponsesWithConfig := make([]*RsvpWithConfig, 0, len(userResponses))
	for _, response := range userResponses {
		config := response.Config
		if config.Title == "" {
			if cachedConfig, ok := configCache[response.ConfigFile]; ok {
				config = cachedConfig
			} else {
				config = formConfig
			}
		}
		allResponsesWithConfig = append(allResponsesWithConfig, &RsvpWithConfig{
			Response: response,
			Config:   config,
		})
	}

	// 构建筛选后的响应（用于表格显示）
	responsesWithConfig := make([]*RsvpWithConfig, 0, len(filteredResponses))
	for _, response := range filteredResponses {
		config := response.Config
		if config.Title == "" {
			if cachedConfig, ok := configCache[response.ConfigFile]; ok {
				config = cachedConfig
			} else {
				config = formConfig
			}
		}
		responsesWithConfig = append(responsesWithConfig, &RsvpWithConfig{
			Response: response,
			Config:   config,
		})
	}

	data := listData{
		Responses:           filteredResponses,
		FormConfig:          displayFormConfig,
		TotalCount:          len(filteredResponses),
		ResponsesWithConfig: responsesWithConfig,
		CurrentUser:         user,
		AvailableConfigs:    availableConfigs,
		SelectedConfig:      selectedConfig,
		AllResponses:        allResponsesWithConfig,
	}
	err := templates["list"].Execute(c.Writer, data)
	if err != nil {
		fmt.Printf("模板执行错误: %v\n", err)
		c.String(500, "模板执行失败: "+err.Error())
		return
	}
}

type formData struct {
	*Rsvp
	Errors     []string
	FormConfig FormConfig
}

func formHandler(c *gin.Context) {
	user := getCurrentUser(c)
	if user == nil {
		c.Redirect(http.StatusFound, "/login")
		return
	}

	formConfigMutex.RLock()
	defer formConfigMutex.RUnlock()

	if c.Request.Method == http.MethodGet {
		for i := range formConfig.Fields {
			formConfig.Fields[i].Value = ""
			if formConfig.Fields[i].Type == "select" && len(formConfig.Fields[i].Options) > 0 {
				for _, option := range formConfig.Fields[i].Options {
					if option.Value != "" {
						formConfig.Fields[i].Value = option.Value
						break
					}
				}
			}
		}

		err := templates["form"].Execute(c.Writer, formData{
			Rsvp: &Rsvp{}, Errors: []string{}, FormConfig: formConfig,
		})
		if err != nil {
			fmt.Printf("模板执行错误: %v\n", err)
			c.String(500, "模板执行失败: "+err.Error())
			return
		}
	} else if c.Request.Method == http.MethodPost {
		c.Request.ParseForm()

		var responseData Rsvp
		responseData.Fields = make(map[string]string)
		responseData.Config = formConfig
		responseData.ConfigFile = currentConfigFile
		responseData.Username = user.Username
		responseData.CreatedAt = time.Now()
		errors := []string{}

		for _, field := range formConfig.Fields {
			value := c.Request.Form.Get(field.Name)

			if field.Type == "checkbox" {
				values := c.Request.Form[field.Name]
				if len(values) > 0 {
					value = strings.Join(values, ",")
				} else {
					value = ""
				}
				fmt.Printf("DEBUG: checkbox field '%s' selected values: %v, joined: '%s'\n", field.Name, values, value)
			}

			for i := range formConfig.Fields {
				if formConfig.Fields[i].Name == field.Name {
					formConfig.Fields[i].Value = value
				}
			}

			if field.Required && strings.TrimSpace(value) == "" {
				errors = append(errors, fmt.Sprintf("请填写%s", field.Label))
				continue
			}

			responseData.Fields[field.Name] = value
		}

		if len(errors) > 0 {
			err := templates["form"].Execute(c.Writer, formData{
				Rsvp: &responseData, Errors: errors, FormConfig: formConfig,
			})
			if err != nil {
				fmt.Printf("模板执行错误: %v\n", err)
				c.String(500, "模板执行失败: "+err.Error())
			}
		} else {
			responsesMutex.Lock()
			responses = append(responses, &responseData)
			responsesMutex.Unlock()

			if err := saveResponseToDB(&responseData); err != nil {
				fmt.Printf("保存响应数据到数据库失败: %v\n", err)
			}

			err := templates["thanks"].Execute(c.Writer, responseData.GetField("name"))
			if err != nil {
				fmt.Printf("模板执行错误: %v\n", err)
				c.String(500, "模板执行失败: "+err.Error())
			}
		}
	}
}

type configManagerData struct {
	CurrentConfig    string
	AvailableConfigs []string
	FormConfig       FormConfig
	CurrentUser      *User
}

func configManagerHandler(c *gin.Context) {
	user := getCurrentUser(c)
	if user == nil {
		c.Redirect(http.StatusFound, "/login")
		return
	}

	formConfigMutex.RLock()
	defer formConfigMutex.RUnlock()

	data := configManagerData{
		CurrentConfig:    currentConfigFile,
		AvailableConfigs: getAvailableConfigs(),
		FormConfig:       formConfig,
		CurrentUser:      user,
	}

	err := templates["config_manager"].Execute(c.Writer, data)
	if err != nil {
		fmt.Printf("模板执行错误: %v\n", err)
		c.String(500, "模板执行失败: "+err.Error())
	}
}

func csvExportHandler(c *gin.Context) {
	user := getCurrentUser(c)
	if user == nil {
		c.Redirect(http.StatusFound, "/login")
		return
	}

	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=\"responses.csv\"")

	csvWriter := csv.NewWriter(c.Writer)
	defer csvWriter.Flush()

	responsesMutex.RLock()
	defer responsesMutex.RUnlock()

	formConfigMutex.RLock()
	defer formConfigMutex.RUnlock()

	var filteredResponses []*Rsvp
	for _, response := range responses {
		if response.ConfigFile == currentConfigFile || response.ConfigFile == "" {
			if user.IsAdmin || response.Username == user.Username {
				filteredResponses = append(filteredResponses, response)
			}
		}
	}

	header := []string{"表单来源", "用户"}
	for _, field := range formConfig.Fields {
		header = append(header, field.Label)
	}
	csvWriter.Write(header)

	for _, response := range filteredResponses {
		row := []string{response.ConfigFile, response.Username}
		for _, field := range formConfig.Fields {
			value := response.GetField(field.Name)
			row = append(row, value)
		}
		csvWriter.Write(row)
	}

	fmt.Printf("成功导出 %d 条记录到 CSV 文件\n", len(filteredResponses))
}

func loginHandler(c *gin.Context) {
	if c.Request.Method == http.MethodGet {
		err := templates["login"].Execute(c.Writer, nil)
		if err != nil {
			fmt.Printf("模板执行错误: %v\n", err)
			c.String(500, "模板执行失败: "+err.Error())
		}
		return
	}

	if c.Request.Method == http.MethodPost {
		username := c.Request.FormValue("username")
		password := c.Request.FormValue("password")

		usersMutex.RLock()
		user, exists := users[username]
		usersMutex.RUnlock()

		if !exists || user.Password != password {
			err := templates["login"].Execute(c.Writer, map[string]string{"Error": "用户名或密码错误"})
			if err != nil {
				fmt.Printf("模板执行错误: %v\n", err)
				c.String(500, "模板执行失败: "+err.Error())
			}
			return
		}

		sessionID := createSession(username)
		c.SetCookie("session_id", sessionID, 3600*24, "/", "", false, true)

		if user.IsAdmin {
			c.Redirect(http.StatusFound, "/config-manager")
		} else {
			c.Redirect(http.StatusFound, "/form")
		}
	}
}

func registerHandler(c *gin.Context) {
	if c.Request.Method == http.MethodGet {
		err := templates["register"].Execute(c.Writer, nil)
		if err != nil {
			fmt.Printf("模板执行错误: %v\n", err)
			c.String(500, "模板执行失败: "+err.Error())
		}
		return
	}

	if c.Request.Method == http.MethodPost {
		username := c.Request.FormValue("username")
		password := c.Request.FormValue("password")
		confirmPassword := c.Request.FormValue("confirm_password")

		if password != confirmPassword {
			err := templates["register"].Execute(c.Writer, map[string]string{"Error": "两次密码不一致"})
			if err != nil {
				fmt.Printf("模板执行错误: %v\n", err)
				c.String(500, "模板执行失败: "+err.Error())
			}
			return
		}

		usersMutex.Lock()
		if _, exists := users[username]; exists {
			usersMutex.Unlock()
			err := templates["register"].Execute(c.Writer, map[string]string{"Error": "用户名已存在"})
			if err != nil {
				fmt.Printf("模板执行错误: %v\n", err)
				c.String(500, "模板执行失败: "+err.Error())
			}
			return
		}

		newUser := &User{
			Username: username,
			Password: password,
			IsAdmin:  false,
		}
		users[username] = newUser
		usersMutex.Unlock()

		if err := saveUserToDB(newUser); err != nil {
			fmt.Printf("保存用户数据失败: %v\n", err)
		}

		sessionID := createSession(username)
		c.SetCookie("session_id", sessionID, 3600*24, "/", "", false, true)
		c.Redirect(http.StatusFound, "/form")
	}
}

func logoutHandler(c *gin.Context) {
	sessionID, err := c.Cookie("session_id")
	if err == nil {
		sessionsMutex.Lock()
		delete(sessions, sessionID)
		sessionsMutex.Unlock()
	}

	c.SetCookie("session_id", "", -1, "/", "", false, true)
	c.Redirect(http.StatusFound, "/login")
}

func main() {
	if err := loadFormConfig(); err != nil {
		panic(err)
	}

	loadTemplates()

	if err := initDB(); err != nil {
		fmt.Printf("数据库初始化失败: %v\n", err)
		panic(err)
	}

	initUsers()

	if err := initResponsesFromCSV(); err != nil {
		fmt.Printf("加载响应数据失败: %v\n", err)
	}

	r := gin.Default()

	r.GET("/", welcomeHandler)
	r.GET("/login", loginHandler)
	r.POST("/login", loginHandler)
	r.GET("/register", registerHandler)
	r.POST("/register", registerHandler)
	r.GET("/logout", logoutHandler)
	r.GET("/list", authMiddleware(), listHandler)
	r.GET("/form", authMiddleware(), formHandler)
	r.POST("/form", authMiddleware(), formHandler)
	r.POST("/switch-config", authMiddleware(), switchConfigHandler)
	r.GET("/config-manager", authMiddleware(), configManagerHandler)
	r.GET("/config_manager", authMiddleware(), configManagerHandler)
	r.GET("/export-csv", authMiddleware(), csvExportHandler)
	r.GET("/pace", runningPaceHandler)
	r.POST("/pace", runningPaceHandler)

	go func() {
		time.Sleep(500 * time.Millisecond)
		openBrowser("http://localhost:8080/login")
	}()

	err := r.Run(":8080")
	if err != nil {
		fmt.Println(err)
	}
}

func openBrowser(url string) {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	default:
		cmd = "xdg-open"
		args = []string{url}
	}

	if err := exec.Command(cmd, args...).Start(); err != nil {
		fmt.Printf("无法打开浏览器: %v\n", err)
	} else {
		fmt.Printf("已打开浏览器: %s\n", url)
	}
}
