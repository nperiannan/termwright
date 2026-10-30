/**
 * Русский. Ключ - английский текст как он написан в программе, значение - перевод.
 * Нет записи - остаётся английский. Подробнее: core/i18n.ts.
 * Термины: session - сессия, jump host - промежуточный узел (jump host),
 * keychain - связка ключей ОС, device type - тип устройства.
 */
const ru: Record<string, string> = {
  // ---- меню ----
  'File': 'Файл', 'Edit': 'Правка', 'View': 'Вид', 'Transfer': 'Передача', 'Script': 'Скрипты', 'Options': 'Настройки', 'Tab': 'Вкладка', 'Help': 'Справка',
  'Connect...': 'Подключиться...', 'Quick Connect...': 'Быстрое подключение...', 'Connect Local Shell': 'Локальная оболочка', 'New Session...': 'Новая сессия...',
  'Reconnect': 'Переподключиться', 'Disconnect': 'Отключиться', 'Clone Session': 'Дублировать сессию', 'Workspaces...': 'Рабочие области...', 'Save Workspace...': 'Сохранить рабочую область...',
  'Log Session...': 'Журнал сессии...', 'Record Session': 'Записать сессию', 'Add Marker to Recording...': 'Добавить метку в запись...', 'Recordings...': 'Записи...',
  'Save Scrollback As...': 'Сохранить прокрутку как...', 'Save Scrollback As HTML...': 'Сохранить прокрутку как HTML...', 'Save Selection As...': 'Сохранить выделенное как...',
  'Save Screen As Image...': 'Сохранить экран как изображение...', 'Import Sessions...': 'Импорт сессий...', 'Discover Devices...': 'Поиск устройств...',
  'Configuration Backups...': 'Резервные копии конфигураций...', 'Change Checklists...': 'Чек-листы изменений...', 'Run and Collect...': 'Выполнить и собрать...', 'Export Sessions...': 'Экспорт сессий...',
  'Close Tab': 'Закрыть вкладку', 'Copy': 'Копировать', 'Paste': 'Вставить', 'Copy and Paste': 'Копировать и вставить', 'Select All': 'Выделить всё', 'Find...': 'Найти...', 'Find': 'Найти',
  'Save Selection as Device Fact...': 'Сохранить выделенное как параметр устройства...', 'Clear Scrollback': 'Очистить прокрутку', 'Clear Screen': 'Очистить экран', 'Reset Terminal': 'Сбросить терминал',
  'Timestamps On / Off': 'Метки времени вкл. / выкл.', 'Auto Scroll On / Off': 'Автопрокрутка вкл. / выкл.', 'Quick Open...': 'Быстрый переход...',
  'Session Manager': 'Менеджер сессий', 'Session Manager Left / Right': 'Менеджер сессий слева / справа', 'Command Window': 'Окно команд', 'Files Panel': 'Панель файлов', 'Button Bar': 'Панель кнопок',
  'Connect Bar': 'Строка подключения', 'Menu Bar in Window': 'Меню в окне', 'Modern / Classic Layout': 'Современный / классический вид', 'Tile / Glyph Icons': 'Плитки / значки',
  'Tabbed': 'Вкладки', 'Tile Side by Side': 'Рядом', 'Tile Stacked': 'Друг под другом', 'Tile Grid': 'Сеткой', 'Zoom In': 'Увеличить', 'Zoom Out': 'Уменьшить', 'Reset Zoom': 'Масштаб по умолчанию',
  'Open SFTP Tab': 'Открыть вкладку SFTP', 'Send File with XMODEM...': 'Отправить файл по XMODEM...', 'Receive File with XMODEM...': 'Принять файл по XMODEM...',
  'Send Files with YMODEM...': 'Отправить файлы по YMODEM...', 'Receive Files with YMODEM': 'Принять файлы по YMODEM', 'Send Text File...': 'Отправить текстовый файл...',
  'Open Web Page Through This Session...': 'Открыть веб-страницу через эту сессию...', 'Wake Up (Wake-on-LAN)': 'Включить (Wake-on-LAN)',
  'SCP Upload File...': 'SCP: загрузить файл...', 'SCP Upload Folder...': 'SCP: загрузить папку...', 'SCP Download...': 'SCP: скачать...',
  'Command Library...': 'Библиотека команд...', 'Command History...': 'История команд...', 'Run Script...': 'Запустить скрипт...', 'Cancel Running Scripts': 'Остановить скрипты',
  'Start / Stop Script Recording': 'Начать / остановить запись скрипта', 'Session Options...': 'Параметры сессии...', 'Global Options...': 'Общие параметры...', 'Passwords & Accounts...': 'Пароли и учётные записи...',
  'Set Up Key Login / Generate Key...': 'Вход по ключу / создать ключ...', 'Set Up Remote Desktop...': 'Настроить удалённый рабочий стол...', 'Compare Outputs...': 'Сравнить вывод...',
  'Next Tab': 'Следующая вкладка', 'Previous Tab': 'Предыдущая вкладка', 'Send Command to All Tabs...': 'Отправить команду во все вкладки...', 'Send Break': 'Отправить Break',
  'Lock Session': 'Заблокировать сессию', 'Lock All Sessions': 'Заблокировать все сессии', 'Welcome...': 'Приветствие...', 'User Guide': 'Руководство пользователя',
  'Keyboard Shortcuts': 'Сочетания клавиш', 'Check for Updates...': 'Проверить обновления...', 'About Termwright': 'О программе Termwright',
  'Quick Connect': 'Быстрое подключение', 'Local Shell': 'Локальная оболочка', 'New Session': 'Новая сессия', 'Global Options': 'Общие параметры', 'Session Options': 'Параметры сессии',

  // ---- главное окно ----
  'Sessions': 'Сессии', 'Import': 'Импорт', 'Discover Devices': 'Поиск устройств', 'Clear': 'Очистить', 'Recent': 'Недавние', 'Recent sessions': 'Недавние сессии',
  'No sessions yet': 'Сессий пока нет', 'Sessions you connect to show up here. Star a session in the Session Manager to keep it at the top.': 'Сессии, к которым вы подключаетесь, появятся здесь. Отметьте сессию звёздочкой в менеджере сессий, чтобы она была в начале списка.',
  'Connect': 'Подключиться', 'Double-click a session in the Session Manager, or:': 'Дважды щёлкните сессию в менеджере сессий или:', 'Import Sessions': 'Импорт сессий',
  'Production': 'Рабочая среда', 'Close': 'Закрыть', 'Open SFTP tab': 'Открыть вкладку SFTP', 'Theme': 'Тема', 'Session locked': 'Сессия заблокирована', 'Lock session': 'Заблокировать сессию',
  'Search or connect…': 'Поиск или подключение…', 'Find in scrollback': 'Поиск в прокрутке', 'Previous (Shift+Enter)': 'Назад (Shift+Enter)', 'Next (Enter)': 'Далее (Enter)', 'Close (Esc)': 'Закрыть (Esc)',
  'No matching sessions': 'Подходящих сессий нет', 'No sessions yet.': 'Сессий пока нет.', 'Create a session': 'Создать сессию', 'or': 'или', 'import sessions from another terminal program': 'импортировать сессии из другой программы',
  'Reconnect automatically': 'Переподключаться автоматически', 'when the connection drops': 'при обрыве соединения',
  'Tabbed view': 'Вкладки', 'Tile side by side': 'Рядом', 'Tile stacked': 'Друг под другом', 'Grid': 'Сетка', 'Log Session': 'Журнал сессии', 'Record script': 'Записать скрипт',
  'Show the Session Manager': 'Показать менеджер сессий', 'Password, then Enter': 'Пароль, затем Enter', 'Stop': 'Стоп', 'Export Sessions': 'Экспорт сессий',

  // ---- менеджер сессий ----
  'New session': 'Новая сессия', 'New folder': 'Новая папка', 'Refresh': 'Обновить', 'Filter sessions': 'Фильтр сессий', 'Favourite': 'Избранное', 'No sessions match': 'Нет подходящих сессий',
  'New session in the selected folder': 'Новая сессия в выбранной папке', 'Import sessions from another terminal program or a file': 'Импорт сессий из другой программы или из файла',
  'Discover devices on a network': 'Найти устройства в сети', 'Filter sessions - or type an address to connect': 'Фильтр сессий - или введите адрес для подключения',
  'Hide the Session Manager (View → Session Manager or the toolbar shows it again)': 'Скрыть менеджер сессий (снова показать: Вид → Менеджер сессий или кнопка на панели)',
  'Two-line rows: name and address, device icon and status (turns Compact Rows off)': 'Двухстрочные строки: имя и адрес, значок устройства и состояние (отключает компактные строки)',

  // ---- быстрое подключение и панель сессии ----
  'Name': 'Имя', 'Folder': 'Папка', 'Protocol': 'Протокол', 'More…': 'Ещё…', 'Serial port': 'Последовательный порт', 'Speed': 'Скорость', 'Baud rate': 'Скорость (бод)',
  'MAC address': 'MAC-адрес', 'UDP port': 'UDP-порт', 'IP address': 'IP-адрес', 'Address': 'Адрес', 'Port': 'Порт', 'Open as': 'Открыть как', 'Terminal': 'Терминал', 'SFTP file browser only': 'Только файловый менеджер SFTP',
  '(none)': '(нет)', 'Account': 'Учётная запись', 'Own username and password': 'Собственные имя и пароль', '+ New Account…': '+ Новая учётная запись…', 'Username': 'Имя пользователя', 'Environment': 'Среда',
  'Password': 'Пароль', 'In keychain': 'В связке ключей', 'Files panel beside the terminal': 'Панель файлов рядом с терминалом', 'Device facts': 'Параметры устройства', 
  'Advanced': 'Дополнительно', 'All session options…': 'Все параметры сессии…', 'Cancel': 'Отмена', 'More Options…': 'Другие параметры…', 'Save': 'Сохранить', 'Save & Connect': 'Сохранить и подключиться', 'e.g. core-sw1': 'например, core-sw1',
  '10.1.1.1 or host.example.com': '10.1.1.1 или host.example.com', 'saved in the system keychain': 'сохранён в связке ключей системы', 'Change the saved password': 'Изменить сохранённый пароль',
  'Connect without saving - or save it as a session': 'Подключиться без сохранения - или сохранить как сессию', '↵ Connect': '↵ Подключиться', 'None': 'Нет', 'change': 'изменить',
  'Save as a session': 'Сохранить как сессию', 'Session name': 'Имя сессии', 'More options…': 'Ещё параметры…', 'Hostname': 'Имя хоста', 'No password': 'Без пароля', 'Save session': 'Сохранить сессию',
  'asked when connecting': 'спрашивается при подключении', '(asked when connecting)': '(спрашивается при подключении)', '(prompt)': '(запрос)', 'host, user@host or host:port': 'хост, user@host или host:port',
  'Type a connect line, or fill in the fields - they follow each other.': 'Введите строку подключения или заполните поля - они связаны между собой.',
  'The shell and folder are set in More options.': 'Оболочка и папка задаются в «Ещё параметры».', 'Detect the speed when connecting': 'Определять скорость при подключении',

  // ---- об окне и справка ----
  'Report an Issue': 'Сообщить о проблеме', 'Releases': 'Релизы', 'Created by': 'Автор', 'Organise': 'Организация', 'Automate': 'Автоматизация',
  'Secure': 'Безопасность', 'Capture': 'Запись', 'Report an issue': 'Сообщить о проблеме', 'Nothing matches.': 'Ничего не найдено.', 'Find a command or a key': 'Найти команду или клавишу',

  // ---- Общие параметры ----
  'Global Options for every session': 'Общие параметры для всех сессий', 'Nothing found': 'Ничего не найдено', 'General': 'Общие', 'Color Schemes': 'Цветовые схемы', 'Keyword Highlighting': 'Подсветка ключевых слов',
  'Automation': 'Автоматизация', 'Key Map': 'Раскладка клавиш', 'Device Types': 'Типы устройств', 'Alerts': 'Оповещения', 'System': 'Система', 'Smart Cards': 'Смарт-карты', 'Updates': 'Обновления',
  'Appearance': 'Внешний вид', 'UI theme': 'Тема интерфейса', 'Follow system': 'Как в системе', 'Dark': 'Тёмная', 'Light': 'Светлая', 'Language': 'Язык', 'Layout': 'Вид окна',
  'Modern': 'Современный', 'Classic': 'Классический', 'Modern - tabs and search in the title bar': 'Современный - вкладки и поиск в заголовке окна', 'Classic - toolbar, tab strip and menu bar': 'Классический - панель инструментов, вкладки и меню',
  "On macOS the window's own title bar changes the next time Termwright starts.": 'В macOS заголовок окна изменится при следующем запуске Termwright.', 'Icons': 'Значки', 'Tiles': 'Плитки', 'Glyphs': 'Символы',
  'Compact Session Manager (one line per session - more sessions in view)': 'Компактный менеджер сессий (одна строка на сессию - больше сессий на экране)', 'Interface font': 'Шрифт интерфейса', 'Text size': 'Размер текста',
  'Accent color': 'Цвет акцента', 'Default': 'По умолчанию', 'Behaviour': 'Поведение', 'Behavior': 'Поведение', 'Confirm before closing connected tabs': 'Спрашивать перед закрытием подключённых вкладок', 'and before quitting': 'и перед выходом',
  'Confirm pasting more than one line': 'Спрашивать при вставке нескольких строк', 'Remember typed commands': 'Запоминать введённые команды', 'Save Quick Connect sessions': 'Сохранять сессии быстрого подключения',
  'Confirm before moving sessions': 'Спрашивать перед перемещением сессий', 'Reopen the tabs that were open': 'Открывать вкладки, которые были открыты', 'when Termwright starts': 'при запуске Termwright',
  'Show Session Manager': 'Показывать менеджер сессий', 'Show Button Bar': 'Показывать панель кнопок', 'Show Command Window': 'Показывать окно команд', 'Show Connect Bar in the toolbar': 'Показывать строку подключения на панели',
  'Folders and files': 'Папки и файлы', 'Sessions folder': 'Папка сессий', 'Browse...': 'Обзор...', 'Logs folder': 'Папка журналов', 'Recordings folder': 'Папка записей', 'Session recording': 'Запись сессий',
  'Downloads folder': 'Папка загрузок', 'SFTP from a terminal': 'SFTP из терминала', 'Python interpreter': 'Интерпретатор Python', 'Data folder': 'Папка данных',
  'Font': 'Шрифт', 'Font size': 'Размер шрифта', 'Line spacing': 'Межстрочный интервал', 'Color scheme': 'Цветовая схема', 'Cursor': 'Курсор', 'Block': 'Блок', 'Underline': 'Подчёркивание', 'Vertical bar': 'Вертикальная черта',
  'Blink': 'Мигание', 'Scrollback lines': 'Строк прокрутки', 'Mouse': 'Мышь', 'Copy on select': 'Копировать при выделении', 'Delete': 'Удалить', 'Import...': 'Импорт...', 'Export...': 'Экспорт...', 'Rename...': 'Переименовать...', 'New...': 'Создать...',
  'OK': 'ОК', 'Style': 'Стиль', 'Default set': 'Набор по умолчанию', 'Network': 'Сеть',

  // ---- Параметры сессии ----
  'Connection': 'Подключение', 'Authentication': 'Аутентификация', 'Jump Host / Proxy': 'Промежуточный узел / прокси', 'Port Forwarding': 'Проброс портов', 'Device Facts': 'Параметры устройства', 'Serial': 'Последовательный порт',
  'Logon Actions': 'Действия при входе', 'Logging': 'Журналирование', 'Value for this session': 'Значение для этой сессии', 'Command': 'Команда', 'Every connect': 'При каждом подключении', 'Once': 'Один раз',
  'Device type': 'Тип устройства', 'Connect as': 'Подключаться как', 'Terminal (SSH2)': 'Терминал (SSH2)', 'Console server': 'Консольный сервер', 'Access': 'Доступ', 'Custom': 'Особый', 'TCP port': 'TCP-порт',
  'Description': 'Описание', 'Domain': 'Домен', 'Window': 'Окно', 'Full screen': 'Во весь экран', 'Size': 'Размер', 'Control': 'Управление', 'Methods': 'Способы',
  'Kerberos principal': 'Субъект Kerberos', 'Service name': 'Имя службы', 'Smart card': 'Смарт-карта', 'Identity file': 'Файл ключа', 'Certificate': 'Сертификат', 'Save password in the OS keychain': 'Сохранить пароль в связке ключей ОС',
  'Forwarding': 'Перенаправление', 'Forward SSH agent': 'Перенаправлять SSH-агент', 'Keep-alive': 'Поддержание соединения', 'Send keep-alive every': 'Отправлять keep-alive каждые', 'seconds': 'сек.',
  'Algorithms': 'Алгоритмы', 'Allow legacy algorithms (for old devices)': 'Разрешить устаревшие алгоритмы (для старых устройств)', 'Key exchange': 'Обмен ключами', 'Ciphers': 'Шифры', 'MACs': 'MAC', 'Host key': 'Ключ хоста',
  'Password manager...': 'Менеджер паролей...', 'Username is needed': 'Требуется имя пользователя',

  // ---- Импорт ----
  'Bring sessions in from another program or an inventory': 'Добавьте сессии из другой программы или из списка устройств', 'Found on this computer': 'Найдено на этом компьютере', 'Use': 'Использовать',
  'Into folder': 'В папку', 'If a name exists': 'Если имя уже есть', 'Keep both': 'Оставить обе', 'Replace': 'Заменить', 'Skip': 'Пропустить', 'Master password': 'Мастер-пароль',
  'Inventories': 'Списки устройств', 'Other terminal apps': 'Другие терминальные программы',

  // ---- общие диалоги ----
  'Yes': 'Да', 'No': 'Нет', 'Apply': 'Применить', 'Open': 'Открыть', 'Add': 'Добавить', 'Remove': 'Удалить', 'Rename': 'Переименовать', 'Search': 'Поиск', 'Settings': 'Настройки', 'Error': 'Ошибка',

  // ---- главное окно (дополнение) ----
  'Favourites': 'Избранное', 'All sessions': 'Все сессии', 'New': 'Создать', 'Scan': 'Поиск', 'Active tab': 'Активная вкладка', 'Choose…': 'Выбрать…', 'Send characters immediately': 'Отправлять символы сразу',
  'History…': 'История…', 'Send ⏎': 'Отправить ⏎', 'Connected': 'Подключено', 'Hide the Session Manager': 'Скрыть менеджер сессий', 'Add button': 'Добавить кнопку', 'Where Send goes': 'Куда отправляется',
  'Choose the tabs to send to': 'Выберите вкладки для отправки', 'Send characters immediately as you type': 'Отправлять символы сразу при вводе', 'Search the commands you typed (Mod+Y)': 'Поиск среди введённых команд (Mod+Y)',
  'All connected ({0})': 'Все подключённые ({0})', 'Selected ({0})': 'Выбранные ({0})', 'Timestamps': 'Метки времени', 'Auto scroll': 'Автопрокрутка',
  'Sessions: every session with its icon - type to filter, or an address to connect. Quick Open (sessions, tabs, commands) is ⌘G.': 'Сессии: все сессии со значками - введите текст для фильтра или адрес для подключения. Быстрый переход (сессии, вкладки, команды) - ⌘G.',
  'Show which devices answer (a quick TCP check of the port of each session, every minute)': 'Показать, какие устройства отвечают (быстрая проверка TCP-порта каждой сессии, раз в минуту)',
  'Switch to the Light theme': 'Включить светлую тему', 'Theme: Dark': 'Тема: тёмная', 'Theme: Light': 'Тема: светлая', 'Open in a tab - connected': 'Открыта во вкладке - подключено',
  '{0} minutes ago': '{0} {0|минуту|минуты|минут} назад', '{0} minute ago': '{0} минуту назад', '{0} hours ago': '{0} {0|час|часа|часов} назад', '{0} hour ago': '{0} час назад', '{0} days ago': '{0} {0|день|дня|дней} назад', '{0} day ago': '{0} день назад',
  'just now': 'только что',

  // ---- Общие параметры (дополнение) ----
  'Search options': 'Поиск параметров', 'Changes apply when you press Save': 'Изменения применяются при нажатии «Сохранить»', 'Terminal & Appearance': 'Терминал и внешний вид', 'System (default)': 'Системный (по умолчанию)',
  'Default (13)': 'По умолчанию (13)', 'Medium (14)': 'Средний (14)', 'Large (15)': 'Крупный (15)', 'Larger (16)': 'Крупнее (16)', 'Largest (17)': 'Самый крупный (17)',
  'Applies to menus, dialogs and the Session Manager; the terminal font is under Terminal & Appearance.': 'Относится к меню, диалогам и менеджеру сессий; шрифт терминала задаётся в разделе «Терминал и внешний вид».',
  'shows the lines first - production tabs always ask': 'сначала показывает строки - вкладки рабочей среды спрашивают всегда',
  'for Command History and suggestions - never commands with passwords, secrets or keys': 'для истории команд и подсказок - команды с паролями, секретами и ключами не запоминаются',
  'every connection made with Quick Connect is kept in the Session Manager (in the selected folder)': 'каждое подключение через «Быстрое подключение» сохраняется в менеджере сессий (в выбранной папке)',
  "dragging or pasting sessions and folders into another folder asks first (Undo is in the Session Manager's ⋯ menu)": 'перетаскивание или вставка сессий и папок в другую папку сначала спрашивает (отмена - в меню ⋯ менеджера сессий)',
  'the session list on the left (or right)': 'список сессий слева (или справа)', 'buttons that send commands, under the terminal': 'кнопки, отправляющие команды, под терминалом',
  'type a command and send it to one or more tabs': 'введите команду и отправьте её в одну или несколько вкладок',
  'Sessions are stored as one .ini file per session, in the layout other terminal programs use too. You can point this at an existing "Config/Sessions" folder to share sessions (back it up first).': 'Сессии хранятся в виде отдельных .ini-файлов, в формате, который используют и другие терминальные программы. Можно указать существующую папку «Config/Sessions», чтобы использовать сессии совместно (сначала сделайте её копию).',
  'Record every connection of every session': 'Записывать каждое подключение каждой сессии', 'Delete recordings after': 'Удалять записи через', 'days (0 = keep them)': 'дн. (0 = не удалять)',
  'SFTP tabs follow the terminal folder': 'Вкладки SFTP следуют за папкой терминала',
  'Working directory detection: turn on folder reporting in the shell (bash, zsh) when an SFTP tab is opened from it': 'Определение текущей папки: при открытии вкладки SFTP из терминала в оболочке (bash, zsh) включается передача текущей папки',
  'SFTP tabs and the Files panel browse as the terminal\'s user (root via sudo after "sudo su -")': 'Вкладки SFTP и панель файлов работают от имени пользователя терминала (root после «sudo su -»)',
  'Files received with ZMODEM (remote "sz") are saved here.': 'Файлы, принятые по ZMODEM (удалённая команда «sz»), сохраняются здесь.',
  '(automatic: python3, or py -3 on Windows)': '(автоматически: python3, в Windows - py -3)', 'Terminal bar': 'Панель терминала', 'Scheme': 'Схема', 'Copy as new...': 'Копировать как новую...',
  'Normal - the font\'s own (as most terminal programs)': 'Обычный - как в шрифте (как в большинстве терминальных программ)', 'Auto (match the UI theme: Default / Light)': 'Авто (по теме интерфейса: Default / Light)',
  'Paste line delay': 'Задержка между строками при вставке', 'ms between lines (0 = off; useful for slow network devices)': 'мс между строками (0 = выкл.; полезно для медленных сетевых устройств)',
  'Edit set': 'Изменить набор', 'Text or pattern': 'Текст или шаблон', 'Regex': 'Регулярное выражение', 'Match case': 'Учитывать регистр', 'Color': 'Цвет', 'Add keyword': 'Добавить слово',

  // ---- Импорт (дополнение) ----
  'Or choose a source - Termwright': 'Или выберите источник - Termwright', 'Source': 'Источник', 'sessions and settings': 'сессии и настройки', 'export folder': 'папка экспорта', 'Config folder': 'папка Config',
  'Sessions folder archive': 'архив папки Sessions', 'Export Settings file': 'файл Export Settings', 'one session file': 'файл одной сессии', 'spreadsheet export': 'экспорт из таблицы', 'INI or YAML inventory': 'список устройств INI или YAML',
  'devices with a primary IP': 'устройства с основным IP', 'via the AWS CLI': 'через AWS CLI', 'Top level': 'Верхний уровень', 'Choose an existing folder or subfolder': 'Выберите существующую папку или подпапку',
  'Termwright export (.zip) - sessions and settings': 'Экспорт Termwright (.zip) - сессии и настройки', 'Termwright export folder': 'Папка экспорта Termwright',
  'SecureCRT configuration folder (Config or Config/Sessions)': 'Папка конфигурации SecureCRT (Config или Config/Sessions)', 'SecureCRT Sessions folder as a .zip archive': 'Папка Sessions SecureCRT в виде .zip-архива',
  'SecureCRT XML settings export (.xml)': 'Экспорт настроек SecureCRT в XML (.xml)', 'SecureCRT single session file (.ini)': 'Файл одной сессии SecureCRT (.ini)',
  'MobaXterm sessions (.mxtsessions export or MobaXterm.ini)': 'Сессии MobaXterm (экспорт .mxtsessions или MobaXterm.ini)', 'mRemoteNG connections (confCons.xml)': 'Подключения mRemoteNG (confCons.xml)',
  'OpenSSH client config (~/.ssh/config)': 'Конфигурация клиента OpenSSH (~/.ssh/config)', 'CSV / spreadsheet export (.csv)': 'Экспорт CSV / таблицы (.csv)', 'Ansible inventory (INI or YAML)': 'Список устройств Ansible (INI или YAML)',
  'NetBox (devices with a primary IP)': 'NetBox (устройства с основным IP)', 'AWS EC2 instances (uses the AWS CLI)': 'Экземпляры AWS EC2 (через AWS CLI)',
};

export default ru;
