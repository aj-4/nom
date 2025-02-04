package commands

import (
	"fmt"
	"io"
	"os"
  "time"

  "github.com/charmbracelet/bubbles/timer"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"

	"github.com/guyfedwards/nom/internal/rss"
)

const listHeight = 14

var (
	appStyle          = lipgloss.NewStyle().Padding(0).Margin(0)
  titleStyle = list.DefaultStyles().Title.Margin(1, 0, 0, 0).Background(lipgloss.Color("#000"))
	itemStyle         = lipgloss.NewStyle().PaddingLeft(4).PaddingRight(1)
	selectedItemStyle = lipgloss.NewStyle().PaddingLeft(2).Foreground(lipgloss.Color("#f1f1f1"))
	paginationStyle   = list.DefaultStyles().PaginationStyle.PaddingLeft(4)
)

type Item struct {
	Title    string
	FeedName string
	URL      string
}

func (i Item) FilterValue() string { return "" }

type itemDelegate struct{}

func (d itemDelegate) Height() int                               { return 1 }
func (d itemDelegate) Spacing() int                              { return 0 }
func (d itemDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd { return nil }
func (d itemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(Item)
	if !ok {
		return
	}

	var str string
	str = fmt.Sprintf("%d. %s", index+1, i.Title)

	fn := itemStyle.Render
	if index == m.Index() {
		fn = func(s string) string {
			return selectedItemStyle.Render("> " + s)
		}
	}

	fmt.Fprint(w, fn(str))
}

type model struct {
	list            list.Model
	commands        Commands
	selectedArticle string
	viewport        viewport.Model
	prevKeyWasG     bool

  timer timer.Model
  OpenBrowser bool
}


func (m model) Init() tea.Cmd {
  return m.timer.Init()
}

func resetTimer(m model, msg tea.Msg) (tea.Model, tea.Cmd) {
    m.list.NewStatusMessage("Fetched at " + time.Now().Format("15:04"))
    m.timer.Timeout = time.Minute * 15

    var cmd tea.Cmd
    m.timer, cmd = m.timer.Update(msg)
    return m, cmd
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// resize all views regardless of which is showing to keep consistent
	// when switching
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		x, y := appStyle.GetFrameSize()

		m.list.SetSize(msg.Width-x, msg.Height-y)

		m.viewport.Width = msg.Width - x
		m.viewport.Height = msg.Height

		return m, nil

  case timer.TickMsg:
			rss, err := m.commands.fetchAllFeeds(true)
			if err != nil {
				return m, tea.Quit
			}

			m.list.SetItems(getItemsFromRSS(rss))

      return resetTimer(m, msg)
  }

	if m.selectedArticle != "" {
		return updateViewport(msg, m)
	}

	return updateList(msg, m)
}

func openArticleInBrowser(m model, i Item) error {
  return m.commands.OpenArticle(i.Title)
}

func updateList(msg tea.Msg, m model) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch keypress := msg.String(); keypress {

		case "ctrl+c":
			return m, tea.Quit

		case "r":
			rss, err := m.commands.fetchAllFeeds(true)
			if err != nil {
				return m, tea.Quit
			}

			m.list.SetItems(getItemsFromRSS(rss))
      return resetTimer(m, msg)

    case "enter":
			i, ok := m.list.SelectedItem().(Item)
			if ok {
        if m.OpenBrowser {
          err := openArticleInBrowser(m, i)
          if err != nil {
            return m, tea.Quit
          }
        } else {
				  m.selectedArticle = i.Title
          m.viewport.GotoTop()

          content, err := m.commands.FindGlamourisedArticle(m.selectedArticle)
          if err != nil {
            return m, tea.Quit
          }

          m.viewport.SetContent(content)
        }

			}

			return m, nil
    }
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func updateViewport(msg tea.Msg, m model) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch keypress := msg.String(); keypress {
		case "g":
			if m.prevKeyWasG {
				m.viewport.GotoTop()
				m.prevKeyWasG = false
			} else {
				m.prevKeyWasG = true
			}
		case "G":
			m.viewport.GotoBottom()
    case "enter":
      i, ok := m.list.SelectedItem().(Item)
      if ok {
        err := openArticleInBrowser(m, i)
        if err != nil {
          return m, tea.Quit
        }
      }
      return m, nil
		case "esc", "q":
			m.selectedArticle = ""

		case "ctrl+c":
			return m, tea.Quit
		}
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m model) View() string {
	var s string

	if m.selectedArticle == "" {
		s = listView(m)
	} else {
		s = viewportView(m)
	}

	return appStyle.Render(s)
}

func listView(m model) string {
	return "\n" + m.list.View()
}

func viewportView(m model) string {
	return m.viewport.View()
}

func RSSToItem(c rss.Item) Item {
	return Item{
		FeedName: c.FeedName,
		Title:    c.Title,
		URL:      c.Link,
	}
}

func Render(items []list.Item, cmds Commands) error {
	const defaultWidth = 20
	_, ts, _ := term.GetSize(int(os.Stdout.Fd()))
	_, y := appStyle.GetFrameSize()
	height := ts - y

	appStyle.Height(height)

  var title string
  var openBrowser bool
  if cmds.config.IsPreviewMode() {
    title = cmds.config.PreviewFeeds[0].Name + " 🍜"
    titleStyle = titleStyle.Background(lipgloss.Color(cmds.config.PreviewFeeds[0].Color))
    openBrowser = cmds.config.PreviewFeeds[0].Browser
  } else {
    title = "nom 🍜"
  }

	l := list.New(items, itemDelegate{}, defaultWidth, height)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
  l.SetShowHelp(false)
	l.Title = title
	l.Styles.Title = titleStyle
	l.Styles.PaginationStyle = paginationStyle
  l.NewStatusMessage("Fetched at " + time.Now().Format("15:04"))
	l.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{
			key.NewBinding(
				key.WithKeys("r"),
				key.WithHelp("r", "refresh cache"),
			),
		}
	}

	vp := viewport.New(78, height)

	m := model{
    list: l, 
    commands: cmds, 
    viewport: vp,
    timer: timer.NewWithInterval(time.Minute * 15, time.Minute * 10),

    OpenBrowser: openBrowser,
  }

	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		return fmt.Errorf("tui.Render: %w", err)
	}

	return nil
}
