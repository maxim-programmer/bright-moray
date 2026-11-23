// Package admincli — команды администрирования из терминала: make admin-create и make admin-delete.
//
// Логика вынесена из main, чтобы её можно было проверить тестами: ввод и вывод передаются явно.
package admincli

import (
	"auction/db"
	"auction/model"
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

// Правила совпадают с регистрацией на сайте (handler/auth.go), чтобы администратор мог войти
// на общей странице входа и затем изменять профиль как обычный пользователь.
var (
	usernameRe = regexp.MustCompile(`^[\p{L}\p{N}_.-]{3,32}$`)
	telegramRe = regexp.MustCompile(`^[A-Za-z0-9_]{5,32}$`)
	emailRe    = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
)

const (
	defaultName  = "Администратор"
	maxNameLen   = 200 // users.name VARCHAR(200)
	maxEmailLen  = 254 // users.email VARCHAR(254)
	minPassLen   = 6
	maxPassBytes = 72 // предел bcrypt
)

// Console — ввод и вывод команды.
type Console struct {
	In  *bufio.Reader
	Out io.Writer
	// Terminal — ввод идёт с терминала: пароль вводится скрыто и подтверждается повтором.
	Terminal bool
}

// NewConsole собирает консоль поверх стандартных потоков.
func NewConsole() *Console {
	st, err := os.Stdin.Stat()
	term := err == nil && st.Mode()&os.ModeCharDevice != 0
	return &Console{In: bufio.NewReader(os.Stdin), Out: os.Stdout, Terminal: term}
}

func (c *Console) readLine(prompt string) (string, error) {
	fmt.Fprint(c.Out, prompt)
	line, err := c.In.ReadString('\n')
	if err != nil && (err != io.EOF || line == "") {
		return "", fmt.Errorf("ввод прерван")
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// readSecret читает пароль без отображения на экране (stty -echo). Если отключить эхо не
// удалось — читает как обычную строку и предупреждает об этом.
func (c *Console) readSecret(prompt string) (string, error) {
	if !c.Terminal {
		return c.readLine(prompt)
	}
	off := exec.Command("stty", "-echo")
	off.Stdin = os.Stdin
	hidden := off.Run() == nil
	if !hidden {
		fmt.Fprintln(c.Out, "(не удалось скрыть ввод — пароль будет виден)")
	} else {
		defer func() {
			on := exec.Command("stty", "echo")
			on.Stdin = os.Stdin
			_ = on.Run()
			fmt.Fprintln(c.Out)
		}()
	}
	return c.readLine(prompt)
}

// readOptional читает необязательное поле: пустая строка и конец ввода означают «пропустить».
func (c *Console) readOptional(prompt string) string {
	line, err := c.readLine(prompt)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(line)
}

// ValidateUsername проверяет логин; возвращает текст ошибки или "".
func ValidateUsername(s string) string {
	if !usernameRe.MatchString(s) {
		return "Username: 3–32 символа — буквы, цифры, «_», «.» и «-»"
	}
	return ""
}

// ValidateEmail проверяет необязательную почту (пустая допустима).
func ValidateEmail(s string) string {
	if s == "" {
		return ""
	}
	if len(s) > maxEmailLen || !emailRe.MatchString(s) {
		return "Почта указана неверно (пример: name@example.com)"
	}
	return ""
}

// NormalizeTelegram убирает «@» и ссылку t.me/ и проверяет необязательный Telegram
// (пустой допустим). Возвращает значение для сохранения и текст ошибки.
func NormalizeTelegram(s string) (string, string) {
	s = strings.TrimSpace(s)
	for _, prefix := range []string{"https://t.me/", "http://t.me/", "t.me/", "@"} {
		s = strings.TrimPrefix(s, prefix)
	}
	if s == "" {
		return "", ""
	}
	if !telegramRe.MatchString(s) {
		return "", "Telegram: 5–32 символа — латинские буквы, цифры и «_»"
	}
	return s, ""
}

func validateName(s string) string {
	if utf8.RuneCountInString(s) > maxNameLen {
		return "Имя не длиннее 200 символов"
	}
	return ""
}

// ValidatePassword — те же правила, что при регистрации.
func ValidatePassword(s string) string {
	switch {
	case len(s) < minPassLen:
		return "Пароль должен быть не короче 6 символов"
	case len(s) > maxPassBytes:
		return "Пароль не должен быть длиннее 72 байт"
	}
	return ""
}

func (c *Console) askUsername() (string, error) {
	login, err := c.readLine("Username: ")
	if err != nil {
		return "", err
	}
	login = strings.TrimSpace(login)
	if msg := ValidateUsername(login); msg != "" {
		return "", errors.New(msg)
	}
	return login, nil
}

// Create — make admin-create:
//  1. запрашивает username;
//  2. запрашивает пароль;
//  3. проверяет, существует ли пользователь с таким username;
//  4. нет — запрашивает имя, почту и Telegram (необязательно) и создаёт пользователя с ролью admin;
//  5. есть — меняет его роль на admin (остальные данные не трогает);
//  6. сохраняет хеш пароля в PostgreSQL.
func Create(ctx context.Context, q *db.Queries, c *Console) error {
	login, err := c.askUsername()
	if err != nil {
		return err
	}
	pass, err := c.readSecret("Пароль: ")
	if err != nil {
		return err
	}
	if msg := ValidatePassword(pass); msg != "" {
		return errors.New(msg)
	}
	if c.Terminal {
		again, err := c.readSecret("Повторите пароль: ")
		if err != nil {
			return err
		}
		if again != pass {
			return errors.New("Пароли не совпадают")
		}
	}

	exists, err := q.CLIUserExists(ctx, login)
	if err != nil {
		return explain(err)
	}
	var profile db.CLIProfile
	if !exists {
		fmt.Fprintln(c.Out, "Пользователя с таким username нет — будет создан новый администратор.")
		fmt.Fprintln(c.Out, "Остальные поля необязательны: Enter — пропустить.")
		profile.Name = c.readOptional("Имя [" + defaultName + "]: ")
		if msg := validateName(profile.Name); msg != "" {
			return errors.New(msg)
		}
		profile.Email = c.readOptional("Почта: ")
		if msg := ValidateEmail(profile.Email); msg != "" {
			return errors.New(msg)
		}
		var msg string
		if profile.Telegram, msg = NormalizeTelegram(c.readOptional("Telegram (username): ")); msg != "" {
			return errors.New(msg)
		}
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("хеширование пароля: %w", err)
	}
	res, err := q.CLIGrantAdmin(ctx, login, string(hash), profile)
	if err != nil {
		return explain(err)
	}
	switch res.Action {
	case db.CLICreated:
		fmt.Fprintf(c.Out, "Готово: создан администратор %q (id %d).\n", res.Username, res.UserID)
	case db.CLIPromoted:
		fmt.Fprintf(c.Out, "Готово: пользователь %q (id %d) теперь администратор, пароль обновлён.\n", res.Username, res.UserID)
	default:
		fmt.Fprintf(c.Out, "Пользователь %q (id %d) уже был администратором: пароль обновлён.\n", res.Username, res.UserID)
	}
	if res.Unblock {
		fmt.Fprintln(c.Out, "С пользователя снята блокировка: администратор не может быть заблокирован.")
	}
	if res.Action != db.CLICreated {
		fmt.Fprintln(c.Out, "Все прежние сессии пользователя завершены.")
	}
	fmt.Fprintln(c.Out, "Вход — на обычной странице входа; кнопка «Панель администратора» ведёт на /admin.")
	return nil
}

// Delete — make admin-delete, откат admin-create. Аккаунт, созданный командой admin-create и
// не имеющий лотов и ставок, удаляется целиком; иначе у пользователя снимается роль admin.
func Delete(ctx context.Context, q *db.Queries, c *Console, assumeYes bool) error {
	login, err := c.askUsername()
	if err != nil {
		return err
	}
	if !assumeYes {
		ans, err := c.readLine(fmt.Sprintf("Откатить администратора %q? [y/N]: ", login))
		if err != nil {
			return err
		}
		if a := strings.ToLower(strings.TrimSpace(ans)); a != "y" && a != "yes" && a != "д" && a != "да" {
			fmt.Fprintln(c.Out, "Отменено.")
			return nil
		}
	}
	res, err := q.CLIRevokeAdmin(ctx, login)
	if err != nil {
		return explain(err)
	}
	switch res.Action {
	case db.CLIDeleted:
		fmt.Fprintf(c.Out, "Готово: администратор %q (id %d) удалён вместе с аккаунтом.\n", res.Username, res.UserID)
	default:
		fmt.Fprintf(c.Out, "Готово: у %q (id %d) снята роль администратора, аккаунт остался обычным пользователем.\n", res.Username, res.UserID)
	}
	return nil
}

// explain переводит ошибки слоя данных в понятные сообщения.
func explain(err error) error {
	var ae *model.APIError
	switch {
	case errors.As(err, &ae):
		return errors.New(ae.Message)
	case errors.Is(err, db.ErrNotFound):
		return errors.New("Пользователь с таким username не найден")
	case errors.Is(err, db.ErrConflict):
		return errors.New("Такой username уже занят")
	}
	return err
}
