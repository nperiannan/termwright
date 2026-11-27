package apiclient

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"
)

// Postman's dynamic variables ({{$randomFirstName}} ...): a new value each time one is used.

func rnd(n int) int {
	if n <= 1 {
		return 0
	}
	v, _ := rand.Int(rand.Reader, big.NewInt(int64(n)))
	return int(v.Int64())
}

func pick(l ...string) string { return l[rnd(len(l))] }

func pickOf(csv string) string { return pick(strings.Split(csv, ",")...) }

func digits(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte('0' + rnd(10))
	}
	return string(b)
}

func alnum(n int, set string) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = set[rnd(len(set))]
	}
	return string(b)
}

const (
	firstNames  = "James,Mary,John,Patricia,Robert,Jennifer,Michael,Linda,William,Elizabeth,David,Barbara,Richard,Susan,Joseph,Jessica,Thomas,Sarah,Charles,Karen,Daniel,Nancy,Matthew,Lisa,Anthony,Betty,Mark,Sandra,Priya,Arjun,Wei,Yuki,Lucas,Emma,Noah,Olivia"
	lastNames   = "Smith,Johnson,Williams,Brown,Jones,Garcia,Miller,Davis,Rodriguez,Martinez,Hernandez,Lopez,Wilson,Anderson,Thomas,Taylor,Moore,Jackson,Martin,Lee,Thompson,White,Harris,Clark,Lewis,Walker,Young,Allen,King,Wright,Kumar,Chen,Tanaka,Silva"
	cities      = "Springfield,Riverside,Franklin,Greenville,Bristol,Clinton,Fairview,Salem,Madison,Georgetown,Arlington,Ashland,Burlington,Manchester,Oxford,Milton,Dover,Hudson,Kingston,Newport"
	countries   = "United States,Canada,Mexico,Brazil,Argentina,United Kingdom,France,Germany,Spain,Italy,Netherlands,Sweden,Norway,India,China,Japan,Australia,New Zealand,South Africa,Egypt"
	countryCode = "US,CA,MX,BR,AR,GB,FR,DE,ES,IT,NL,SE,NO,IN,CN,JP,AU,NZ,ZA,EG"
	streets     = "Main,Oak,Pine,Maple,Cedar,Elm,Washington,Lake,Hill,Park,View,Sunset,River,Church,Mill,Spring,Ridge,Forest,Meadow,Valley"
	streetSuf   = "Street,Avenue,Road,Lane,Drive,Court,Place,Way,Boulevard,Terrace"
	nouns       = "system,network,router,switch,port,interface,protocol,bandwidth,array,matrix,circuit,driver,card,firewall,pixel,panel,feed,monitor,sensor,program,application,capacitor,transmitter,alarm,bus,microchip,hard drive"
	verbs       = "connect,parse,index,compress,generate,navigate,transmit,back up,override,calculate,quantify,program,reboot,bypass,input,copy,hack,synthesize"
	ingverbs    = "connecting,parsing,indexing,compressing,generating,navigating,transmitting,backing up,overriding,calculating,quantifying,programming,rebooting,bypassing,synthesizing"
	adjectives  = "auxiliary,primary,back-end,digital,open-source,virtual,cross-platform,redundant,online,haptic,multi-byte,bluetooth,wireless,1080p,neural,optical,solid state,mobile"
	lorem       = "lorem,ipsum,dolor,sit,amet,consectetur,adipiscing,elit,sed,do,eiusmod,tempor,incididunt,ut,labore,et,dolore,magna,aliqua,enim,ad,minim,veniam,quis,nostrud,exercitation,ullamco,laboris,nisi,aliquip,ex,ea,commodo,consequat"
	companySuf  = "Inc,LLC,Group,and Sons,Ltd,Corp"
	jobArea     = "Solutions,Program,Brand,Security,Research,Marketing,Directives,Implementation,Integration,Functionality,Response,Paradigm,Tactics,Identity,Markets,Group,Division,Applications,Optimization,Operations,Infrastructure,Intranet,Communications,Web,Branding,Quality,Assurance,Mobility,Accounts,Factors,Creative,Configuration,Accountability,Interactions,Usability,Metrics"
	jobDesc     = "Lead,Senior,Direct,Corporate,Dynamic,Future,Product,National,Regional,District,Central,Global,Customer,Investor,Dynamic,International,Legacy,Forward,Internal,Human,Chief,Principal"
	jobType     = "Supervisor,Associate,Executive,Liaison,Officer,Manager,Engineer,Specialist,Director,Coordinator,Administrator,Architect,Analyst,Designer,Planner,Orchestrator,Technician,Developer,Producer,Consultant,Assistant,Facilitator,Agent,Representative,Strategist"
	productAdj  = "Small,Ergonomic,Rustic,Intelligent,Gorgeous,Incredible,Fantastic,Practical,Sleek,Awesome,Generic,Handcrafted,Handmade,Licensed,Refined,Unbranded,Tasty"
	productMat  = "Steel,Wooden,Concrete,Plastic,Cotton,Granite,Rubber,Metal,Soft,Fresh,Frozen"
	products    = "Chair,Car,Computer,Keyboard,Mouse,Bike,Ball,Gloves,Pants,Shirt,Table,Shoes,Hat,Towels,Soap,Tuna,Chicken,Fish,Cheese,Bacon,Pizza,Salad,Sausages,Chips"
	departments = "Books,Movies,Music,Games,Electronics,Computers,Home,Garden,Tools,Grocery,Health,Beauty,Toys,Kids,Baby,Clothing,Shoes,Jewelery,Sports,Outdoors,Automotive,Industrial"
	colors      = "red,green,blue,yellow,purple,orange,black,white,gray,cyan,magenta,lime,maroon,navy,olive,teal,silver,gold,indigo,violet"
	currencies  = "USD:US Dollar:$,EUR:Euro:€,GBP:Pound Sterling:£,JPY:Yen:¥,INR:Indian Rupee:₹,CAD:Canadian Dollar:$,AUD:Australian Dollar:$,CHF:Swiss Franc:CHF,CNY:Yuan Renminbi:¥,BRL:Brazilian Real:R$"
	fileTypes   = "json:application/json,txt:text/plain,pdf:application/pdf,png:image/png,jpg:image/jpeg,csv:text/csv,xml:application/xml,html:text/html,zip:application/zip,mp4:video/mp4,mp3:audio/mpeg,gif:image/gif"
	domainSuf   = "com,net,org,info,biz,io,name"
	locales     = "en,en_US,en_GB,de,fr,es,it,ja,ko,nl,pt_BR,ru,sv,zh_CN,hi"
	abbrevs     = "TCP,HTTP,SDD,RAM,GB,CSS,SSL,AGP,SQL,FTP,PCI,AI,ADP,RSS,XML,EXE,COM,HDD,THX,SMTP,SMS,USB,PNG,SAS,IB,SCSI,JSON,XSS,JBOD"
	dbColumns   = "id,title,name,email,phone,token,group,category,password,comment,avatar,status,createdAt,updatedAt"
	dbTypes     = "int,varchar,text,date,datetime,tinyint,time,timestamp,smallint,mediumint,bigint,decimal,float,double,real,bit,boolean,serial,blob,binary,enum,set,geometry,point"
	dbEngines   = "InnoDB,MyISAM,MEMORY,CSV,BLACKHOLE,ARCHIVE"
	txTypes     = "deposit,withdrawal,payment,invoice"
	userAgents  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36|Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15|Mozilla/5.0 (X11; Linux x86_64; rv:121.0) Gecko/20100101 Firefox/121.0"
)

func cap1(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func loremWords(n int) string {
	w := make([]string, n)
	for i := range w {
		w[i] = pickOf(lorem)
	}
	return strings.Join(w, " ")
}

func loremSentence() string { return cap1(loremWords(4+rnd(6))) + "." }

func loremSentences(n int) string {
	s := make([]string, n)
	for i := range s {
		s[i] = loremSentence()
	}
	return strings.Join(s, " ")
}

func loremParagraphs(n int, sep string) string {
	p := make([]string, n)
	for i := range p {
		p[i] = loremSentences(3)
	}
	return strings.Join(p, sep)
}

func ipv4() string { return fmt.Sprintf("%d.%d.%d.%d", 1+rnd(223), rnd(256), rnd(256), 1+rnd(254)) }

func pastDate(maxDays int) time.Time {
	return time.Now().Add(-time.Duration(1+rnd(maxDays*24*3600)) * time.Second)
}

// dynamicVar returns the value of a Postman dynamic variable ("$randomEmail"), false when it is not one.
func dynamicVar(k string) (string, bool) {
	first, last := pickOf(firstNames), pickOf(lastNames)
	cur := strings.Split(pickOf(currencies), ":")
	ft := strings.Split(pickOf(fileTypes), ":")
	word := pickOf(nouns)
	switch k {
	case "$guid", "$randomUUID":
		return uuid(), true
	case "$timestamp":
		return strconv.FormatInt(time.Now().Unix(), 10), true
	case "$isoTimestamp":
		return time.Now().UTC().Format("2006-01-02T15:04:05.000Z"), true
	case "$randomInt":
		return strconv.Itoa(rnd(1001)), true // Postman: 0 to 1000
	case "$randomAlphaNumeric":
		return alnum(1, "abcdefghijklmnopqrstuvwxyz0123456789"), true
	case "$randomBoolean":
		return pick("true", "false"), true
	case "$randomColor":
		return pickOf(colors), true
	case "$randomHexColor":
		return fmt.Sprintf("#%06x", rnd(1<<24)), true
	case "$randomAbbreviation":
		return pickOf(abbrevs), true
	case "$randomIP":
		return ipv4(), true
	case "$randomIPV6":
		p := make([]string, 8)
		for i := range p {
			p[i] = fmt.Sprintf("%x", rnd(65536))
		}
		return strings.Join(p, ":"), true
	case "$randomMACAddress":
		p := make([]string, 6)
		for i := range p {
			p[i] = fmt.Sprintf("%02x", rnd(256))
		}
		return strings.Join(p, ":"), true
	case "$randomPassword":
		return alnum(15, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"), true
	case "$randomLocale":
		return pickOf(locales), true
	case "$randomUserAgent":
		return pick(strings.Split(userAgents, "|")...), true
	case "$randomProtocol":
		return pick("http", "https"), true
	case "$randomSemver":
		return fmt.Sprintf("%d.%d.%d", rnd(10), rnd(10), rnd(10)), true
	case "$randomFirstName":
		return first, true
	case "$randomLastName":
		return last, true
	case "$randomFullName":
		return first + " " + last, true
	case "$randomNamePrefix":
		return pick("Mr.", "Mrs.", "Ms.", "Miss", "Dr."), true
	case "$randomNameSuffix":
		return pick("Jr.", "Sr.", "I", "II", "III", "IV", "V", "MD", "DDS", "PhD", "DVM"), true
	case "$randomJobArea":
		return pickOf(jobArea), true
	case "$randomJobDescriptor":
		return pickOf(jobDesc), true
	case "$randomJobType":
		return pickOf(jobType), true
	case "$randomJobTitle":
		return pickOf(jobDesc) + " " + pickOf(jobArea) + " " + pickOf(jobType), true
	case "$randomPhoneNumber":
		return fmt.Sprintf("%s-%s-%s", digits(3), digits(3), digits(4)), true
	case "$randomPhoneNumberExt":
		return fmt.Sprintf("%s-%s-%s-%s", digits(2), digits(3), digits(3), digits(4)), true
	case "$randomCity":
		return pickOf(cities), true
	case "$randomStreetName":
		return pickOf(streets) + " " + pickOf(streetSuf), true
	case "$randomStreetAddress":
		return fmt.Sprintf("%d %s %s", 1+rnd(9999), pickOf(streets), pickOf(streetSuf)), true
	case "$randomCountry":
		return pickOf(countries), true
	case "$randomCountryCode":
		return pickOf(countryCode), true
	case "$randomLatitude":
		return fmt.Sprintf("%.4f", float64(rnd(1800000))/10000-90), true
	case "$randomLongitude":
		return fmt.Sprintf("%.4f", float64(rnd(3600000))/10000-180), true
	case "$randomAvatarImage":
		return fmt.Sprintf("https://i.pravatar.cc/150?img=%d", 1+rnd(70)), true
	case "$randomImageUrl", "$randomAbstractImage", "$randomAnimalsImage", "$randomBusinessImage", "$randomCatsImage", "$randomCityImage", "$randomFoodImage",
		"$randomNightlifeImage", "$randomFashionImage", "$randomPeopleImage", "$randomNatureImage", "$randomSportsImage", "$randomTransportImage":
		return fmt.Sprintf("https://picsum.photos/seed/%s/640/480", alnum(8, "abcdefghijklmnopqrstuvwxyz")), true
	case "$randomImageDataUri":
		return "data:image/svg+xml;charset=UTF-8,%3Csvg%20xmlns%3D%22http%3A%2F%2Fwww.w3.org%2F2000%2Fsvg%22%20width%3D%2264%22%20height%3D%2264%22%3E%3Crect%20width%3D%22100%25%22%20height%3D%22100%25%22%20fill%3D%22grey%22%2F%3E%3C%2Fsvg%3E", true
	case "$randomDomainName":
		return strings.ToLower(last) + "." + pickOf(domainSuf), true
	case "$randomDomainSuffix":
		return pickOf(domainSuf), true
	case "$randomDomainWord":
		return strings.ToLower(first + "-" + last), true
	case "$randomEmail":
		return strings.ToLower(first+"."+last) + digits(2) + "@" + pick("gmail.com", "yahoo.com", "hotmail.com"), true
	case "$randomExampleEmail":
		return strings.ToLower(first+"."+last) + digits(2) + "@" + pick("example.com", "example.net", "example.org"), true
	case "$randomUserName":
		return first + "." + last + digits(2), true
	case "$randomUrl":
		return "https://" + strings.ToLower(last) + "." + pickOf(domainSuf), true
	case "$randomFileName", "$randomCommonFileName":
		return strings.ReplaceAll(word, " ", "_") + "_" + pickOf(lorem) + "." + ft[0], true
	case "$randomFileType", "$randomCommonFileType":
		return strings.Split(ft[1], "/")[0], true
	case "$randomFileExt", "$randomCommonFileExt":
		return ft[0], true
	case "$randomFilePath":
		return "/" + pickOf(lorem) + "/" + pickOf(lorem) + "/" + pickOf(lorem) + "." + ft[0], true
	case "$randomDirectoryPath":
		return "/" + pickOf(lorem) + "/" + pickOf(lorem), true
	case "$randomMimeType":
		return ft[1], true
	case "$randomPrice":
		return fmt.Sprintf("%d.%02d", rnd(1000), rnd(100)), true
	case "$randomCurrencyCode":
		return cur[0], true
	case "$randomCurrencyName":
		return cur[1], true
	case "$randomCurrencySymbol":
		return cur[2], true
	case "$randomBitcoin":
		return "1" + alnum(32, "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"), true
	case "$randomBankAccount":
		return digits(8), true
	case "$randomBankAccountName":
		return pick("Home Loan", "Checking", "Savings", "Money Market", "Credit Card", "Auto Loan", "Personal Loan", "Investment") + " Account", true
	case "$randomCreditCardMask":
		return digits(4), true
	case "$randomBankAccountBic":
		return alnum(4, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") + pickOf(countryCode) + alnum(2, "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"), true
	case "$randomBankAccountIban":
		return pickOf(countryCode) + digits(2) + alnum(4, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") + digits(14), true
	case "$randomTransactionType":
		return pickOf(txTypes), true
	case "$randomCompanyName":
		return last + " " + pickOf(companySuf), true
	case "$randomCompanySuffix":
		return pickOf(companySuf), true
	case "$randomBs", "$randomCatchPhrase":
		return cap1(pickOf(adjectives)) + " " + pickOf(ingverbs) + " " + pickOf(nouns), true
	case "$randomBsAdjective", "$randomCatchPhraseAdjective":
		return pickOf(adjectives), true
	case "$randomBsBuzz", "$randomCatchPhraseDescriptor":
		return pickOf(verbs), true
	case "$randomBsNoun", "$randomCatchPhraseNoun":
		return pickOf(nouns), true
	case "$randomDatabaseColumn":
		return pickOf(dbColumns), true
	case "$randomDatabaseType":
		return pickOf(dbTypes), true
	case "$randomDatabaseCollation":
		return pick("utf8_unicode_ci", "utf8_general_ci", "utf8_bin", "ascii_bin", "ascii_general_ci", "cp1250_bin", "cp1250_general_ci"), true
	case "$randomDatabaseEngine":
		return pickOf(dbEngines), true
	case "$randomDateFuture":
		return time.Now().Add(time.Duration(1+rnd(365*24*3600)) * time.Second).UTC().Format(time.RFC1123), true
	case "$randomDatePast":
		return pastDate(365).UTC().Format(time.RFC1123), true
	case "$randomDateRecent":
		return pastDate(1).UTC().Format(time.RFC1123), true
	case "$randomWeekday":
		return time.Weekday(rnd(7)).String(), true
	case "$randomMonth":
		return time.Month(1 + rnd(12)).String(), true
	case "$randomProductName":
		return pickOf(productAdj) + " " + pickOf(productMat) + " " + pickOf(products), true
	case "$randomProduct":
		return pickOf(products), true
	case "$randomProductAdjective":
		return pickOf(productAdj), true
	case "$randomProductMaterial":
		return pickOf(productMat), true
	case "$randomDepartment":
		return pickOf(departments), true
	case "$randomNoun":
		return pickOf(nouns), true
	case "$randomVerb":
		return pickOf(verbs), true
	case "$randomIngverb":
		return pickOf(ingverbs), true
	case "$randomAdjective":
		return pickOf(adjectives), true
	case "$randomWord":
		return pick(pickOf(nouns), pickOf(verbs), pickOf(adjectives)), true
	case "$randomWords":
		return pickOf(adjectives) + " " + pickOf(nouns) + " " + pickOf(verbs), true
	case "$randomPhrase":
		return cap1(pickOf(ingverbs)) + " the " + pickOf(nouns) + " won't do anything, we need to " + pickOf(verbs) + " the " + pickOf(adjectives) + " " + pickOf(nouns) + "!", true
	case "$randomLoremWord":
		return pickOf(lorem), true
	case "$randomLoremWords":
		return loremWords(3), true
	case "$randomLoremSentence":
		return loremSentence(), true
	case "$randomLoremSentences":
		return loremSentences(2 + rnd(4)), true
	case "$randomLoremParagraph":
		return loremParagraphs(1, ""), true
	case "$randomLoremParagraphs":
		return loremParagraphs(3, "\n \r"), true
	case "$randomLoremText":
		return loremSentences(4), true
	case "$randomLoremSlug":
		return strings.ReplaceAll(loremWords(3), " ", "-"), true
	case "$randomLoremLines":
		return strings.Join([]string{loremSentence(), loremSentence(), loremSentence()}, "\n"), true
	}
	return "", false
}

// DynamicVarNames lists the dynamic variables (for suggestions).
func DynamicVarNames() []string {
	return strings.Split("$guid,$timestamp,$isoTimestamp,$randomUUID,$randomInt,$randomAlphaNumeric,$randomBoolean,$randomColor,$randomHexColor,$randomAbbreviation,$randomIP,$randomIPV6,$randomMACAddress,$randomPassword,$randomLocale,$randomUserAgent,$randomProtocol,$randomSemver,$randomFirstName,$randomLastName,$randomFullName,$randomNamePrefix,$randomNameSuffix,$randomJobArea,$randomJobDescriptor,$randomJobTitle,$randomJobType,$randomPhoneNumber,$randomPhoneNumberExt,$randomCity,$randomStreetName,$randomStreetAddress,$randomCountry,$randomCountryCode,$randomLatitude,$randomLongitude,$randomAvatarImage,$randomImageUrl,$randomImageDataUri,$randomDomainName,$randomDomainSuffix,$randomDomainWord,$randomEmail,$randomExampleEmail,$randomUserName,$randomUrl,$randomFileName,$randomFileType,$randomFileExt,$randomCommonFileName,$randomCommonFileType,$randomCommonFileExt,$randomFilePath,$randomDirectoryPath,$randomMimeType,$randomPrice,$randomCurrencyCode,$randomCurrencyName,$randomCurrencySymbol,$randomBitcoin,$randomBankAccount,$randomBankAccountName,$randomCreditCardMask,$randomBankAccountBic,$randomBankAccountIban,$randomTransactionType,$randomCompanyName,$randomCompanySuffix,$randomBs,$randomBsAdjective,$randomBsBuzz,$randomBsNoun,$randomCatchPhrase,$randomCatchPhraseAdjective,$randomCatchPhraseDescriptor,$randomCatchPhraseNoun,$randomDatabaseColumn,$randomDatabaseType,$randomDatabaseCollation,$randomDatabaseEngine,$randomDateFuture,$randomDatePast,$randomDateRecent,$randomWeekday,$randomMonth,$randomProductName,$randomProduct,$randomProductAdjective,$randomProductMaterial,$randomDepartment,$randomNoun,$randomVerb,$randomIngverb,$randomAdjective,$randomWord,$randomWords,$randomPhrase,$randomLoremWord,$randomLoremWords,$randomLoremSentence,$randomLoremSentences,$randomLoremParagraph,$randomLoremParagraphs,$randomLoremText,$randomLoremSlug,$randomLoremLines", ",")
}
