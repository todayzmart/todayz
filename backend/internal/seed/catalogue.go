package seed

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/oklog/ulid/v2"

	"github.com/todayz/backend/internal/catalogue"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/ids"
)

// The catalogue seed (W1-05): the master catalogue (about 3,000 products,
// generated from the lists below; no network, no model), and the items, stock
// and batches of every live seeded store, with the manual book's fixtures
// (docs/qa/manual/README.md §6–7: SL I1–I14, SL2 J1–J22, AP, NH, KF, AM, HY,
// MM and the twelve Chennai neighbours of <seed:price-compare>).
//
// Every id is fixed (products MP, brands MR, store items MQ, movements and
// batches from a counter), every time comes from the seed clock, and stock
// goes in through the stock writer, so movements, batches and the cached
// counters agree exactly as they do in service. A second run on the same
// database changes nothing: rows are ON CONFLICT DO NOTHING and an item that
// already exists gets no stock.
//
// Sales history (<seed:history-SL>, <seed:insights-SL2>: the bills behind
// J1's 64 sales, J11's sell-out, the J12–J17 "sale of 1") belongs to the POS
// and orders seeds; they write sales through catalogue.Writer, which moves
// these counters. The pending master product (<seed:pending-product>) waits
// for the review queue, which the weekend slice defers.

// Seeded id prefixes (Crockford base32; the identity and business seeds use
// M9, MB, MS and MT).
const (
	productIDPrefix   = "01KE0S1W6W8ZV7T1C3A6MP"
	brandIDPrefix     = "01KE0S1W6W8ZV7T1C3A6MR"
	storeItemIDPrefix = "01KE0S1W6W8ZV7T1C3A6MQ"
)

const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// seqID is prefix + n as four Crockford digits.
func seqID(prefix string, n int) ids.ID {
	if n < 0 || n >= 32*32*32*32 {
		panic(fmt.Sprintf("seed: id number %d out of range", n))
	}
	var b [4]byte
	for i := 3; i >= 0; i-- {
		b[i] = crockford[n%32]
		n /= 32
	}
	return mustID(prefix + string(b[:]))
}

// categoryIDs are the migration's fixed category ids (10501), by slug.
var categoryIDs = map[string]string{
	"staples": "101", "dairy-eggs": "102", "fruits-vegetables": "103", "snacks": "104",
	"beverages": "105", "spices-oils": "106", "bakery": "107", "household": "108",
	"personal-care": "109", "medicines": "201", "health-wellness": "202", "baby-care": "203",
	"skin-care": "301", "hair-care": "302", "makeup": "303", "mobile-accessories": "401",
	"batteries-lighting": "402", "small-appliances": "403", "school-supplies": "501",
	"office-supplies": "502", "tools": "601", "electrical": "602", "plumbing": "603", "paints": "604",
}

func categoryID(slug string) string {
	n, ok := categoryIDs[slug]
	if !ok {
		panic("seed: no category " + slug)
	}
	return "0190c47e-0000-7000-8000-000000000" + n
}

// --- products ---------------------------------------------------------------------

// seedProduct is one master product. Name is the full display name with the
// pack ("Amul Butter 100 g"): the manual book, the search log's product match
// (W1-06: the normalised query equals a product name) and the asks all name
// products that way.
type seedProduct struct {
	Key      string // the book's name for a fixture product; "" for generated ones
	Name     string
	Brand    string
	Category string
	Pack     float64
	Unit     string
	MRP      int64 // paise
	Barcode  string
}

// fixtureProducts are the products the manual book names (§6–7), ids 1….
// Their order fixes their ids: append only.
var fixtureProducts = []seedProduct{
	{Name: "Amul Butter 100 g", Brand: "Amul", Category: "dairy-eggs", Pack: 100, Unit: "g", MRP: 6000, Barcode: "8901262010016"},
	{Name: "Maggi 2-Minute Noodles 70 g", Brand: "Maggi", Category: "snacks", Pack: 70, Unit: "g", MRP: 1500, Barcode: "8901058000177"},
	{Name: "Nandini Toned Milk 500 ml", Brand: "Nandini", Category: "dairy-eggs", Pack: 500, Unit: "ml", MRP: 2400},
	{Name: "Wheat atta 5 kg", Category: "staples", Pack: 5, Unit: "kg", MRP: 26000},
	{Name: "Coca-Cola 750 ml", Brand: "Coca-Cola", Category: "beverages", Pack: 750, Unit: "ml", MRP: 4000},
	{Name: "Dolo 650 strip", Brand: "Dolo", Category: "medicines", Pack: 1, Unit: "pack", MRP: 3200},
	{Name: "Toor dal 1 kg", Category: "staples", Pack: 1, Unit: "kg", MRP: 17000},
	{Name: "Curd 400 g", Category: "dairy-eggs", Pack: 400, Unit: "g", MRP: 3500},
	{Name: "QA pack ₹99.99", Category: "household", Pack: 1, Unit: "pack", MRP: 9999},
	{Name: "QA pack ₹100.00", Category: "household", Pack: 1, Unit: "pack", MRP: 10000},
	{Name: "QA pack ₹100.01", Category: "household", Pack: 1, Unit: "pack", MRP: 10001},
	{Name: "QA pack ₹0.50", Category: "household", Pack: 1, Unit: "pack", MRP: 50},
	{Name: "QA pack ₹120", Category: "household", Pack: 1, Unit: "pack", MRP: 12000},
	{Name: "QA pack ₹487", Category: "household", Pack: 1, Unit: "pack", MRP: 48700},
	// SL2 (J1–J22)
	{Name: "Tata Tea Gold 250 g", Brand: "Tata Tea", Category: "beverages", Pack: 250, Unit: "g", MRP: 15000},
	{Name: "Curd 200 g", Category: "dairy-eggs", Pack: 200, Unit: "g", MRP: 2000},
	{Name: "Buttermilk 200 ml", Category: "dairy-eggs", Pack: 200, Unit: "ml", MRP: 1500},
	{Name: "Cheese slices 200 g", Category: "dairy-eggs", Pack: 200, Unit: "g", MRP: 12500},
	{Name: "Fresh cream 200 ml", Category: "dairy-eggs", Pack: 200, Unit: "ml", MRP: 5500},
	{Name: "Paneer 200 g", Category: "dairy-eggs", Pack: 200, Unit: "g", MRP: 9500},
	{Name: "Sunflower oil 1 L", Category: "spices-oils", Pack: 1, Unit: "l", MRP: 15500},
	{Name: "Mango pickle 500 g", Category: "spices-oils", Pack: 500, Unit: "g", MRP: 7000},
	{Name: "Agarbatti pack", Category: "household", Pack: 1, Unit: "pack", MRP: 3000},
	{Name: "Papad 200 g", Category: "snacks", Pack: 200, Unit: "g", MRP: 5000},
	{Name: "Idli rice 1 kg", Category: "staples", Pack: 1, Unit: "kg", MRP: 7500},
	{Name: "Ragi flour 500 g", Category: "staples", Pack: 500, Unit: "g", MRP: 5000},
	{Name: "Poha 500 g", Category: "staples", Pack: 500, Unit: "g", MRP: 4000},
	{Name: "Jaggery 500 g", Category: "staples", Pack: 500, Unit: "g", MRP: 6500},
	{Name: "Sabudana 500 g", Category: "staples", Pack: 500, Unit: "g", MRP: 6000},
	{Name: "Vermicelli 400 g", Category: "staples", Pack: 400, Unit: "g", MRP: 4500},
	{Name: "Rice flour 1 kg", Category: "staples", Pack: 1, Unit: "kg", MRP: 6500},
	{Name: "Bread 400 g", Category: "bakery", Pack: 400, Unit: "g", MRP: 5000},
	{Name: "Eggs tray 12", Category: "dairy-eggs", Pack: 12, Unit: "pc", MRP: 9000},
	{Name: "Parle-G", Brand: "Parle", Category: "snacks", Pack: 1, Unit: "pack", MRP: 1000},
	{Name: "Salt 1 kg", Category: "staples", Pack: 1, Unit: "kg", MRP: 2800},
	// AP, asks, wanted nearby and ask gaps
	{Name: "Boost 500 g", Brand: "Boost", Category: "beverages", Pack: 500, Unit: "g", MRP: 28000},
	{Name: "LED bulb 9 W", Category: "electrical", Pack: 1, Unit: "pc", MRP: 12000},
	{Name: "Extension board 4 socket", Category: "electrical", Pack: 1, Unit: "pc", MRP: 45000},
	{Name: "Horlicks 500 g", Brand: "Horlicks", Category: "beverages", Pack: 500, Unit: "g", MRP: 27000},
	{Name: "Bournvita 500 g", Brand: "Bournvita", Category: "beverages", Pack: 500, Unit: "g", MRP: 25000},
	{Name: "Kodo millet 1 kg", Category: "staples", Pack: 1, Unit: "kg", MRP: 16000},
	{Name: "Foxtail millet 500 g", Category: "staples", Pack: 500, Unit: "g", MRP: 9000},
	// MM and the Chennai neighbours
	{Name: "Sugar 1 kg", Category: "staples", Pack: 1, Unit: "kg", MRP: 6500},
	{Name: "Jaggery 1 kg", Category: "staples", Pack: 1, Unit: "kg", MRP: 10000},
	// The everyday pool the other stores list.
	{Name: "Sona masoori rice 5 kg", Category: "staples", Pack: 5, Unit: "kg", MRP: 38000},
	{Name: "Moong dal 1 kg", Category: "staples", Pack: 1, Unit: "kg", MRP: 15000},
	{Name: "Chana dal 1 kg", Category: "staples", Pack: 1, Unit: "kg", MRP: 11000},
	{Name: "Groundnut oil 1 L", Category: "spices-oils", Pack: 1, Unit: "l", MRP: 22000},
	{Name: "Turmeric powder 100 g", Category: "spices-oils", Pack: 100, Unit: "g", MRP: 4000},
	{Name: "Chilli powder 100 g", Category: "spices-oils", Pack: 100, Unit: "g", MRP: 4500},
	{Name: "Filter coffee powder 200 g", Category: "beverages", Pack: 200, Unit: "g", MRP: 16000},
	{Name: "Rusk 300 g", Category: "bakery", Pack: 300, Unit: "g", MRP: 6000},
	{Name: "Bath soap 100 g", Category: "personal-care", Pack: 100, Unit: "g", MRP: 4500},
	{Name: "Toothpaste 150 g", Category: "personal-care", Pack: 150, Unit: "g", MRP: 11000},
	{Name: "Detergent powder 1 kg", Category: "household", Pack: 1, Unit: "kg", MRP: 13000},
	{Name: "Dishwash bar 200 g", Category: "household", Pack: 200, Unit: "g", MRP: 2500},
	{Name: "Floor cleaner 1 L", Category: "household", Pack: 1, Unit: "l", MRP: 20000},
	{Name: "Cream biscuits 100 g", Category: "snacks", Pack: 100, Unit: "g", MRP: 3000},
	{Name: "Potato chips 52 g", Category: "snacks", Pack: 52, Unit: "g", MRP: 2000},
	{Name: "Namkeen mixture 200 g", Category: "snacks", Pack: 200, Unit: "g", MRP: 6000},
	{Name: "Coconut oil 500 ml", Category: "spices-oils", Pack: 500, Unit: "ml", MRP: 14000},
	{Name: "Mustard seeds 100 g", Category: "spices-oils", Pack: 100, Unit: "g", MRP: 2500},
	{Name: "Tamarind 250 g", Category: "spices-oils", Pack: 250, Unit: "g", MRP: 6000},
	{Name: "Matchbox pack of 10", Category: "household", Pack: 10, Unit: "pc", MRP: 2000},
	{Name: "Hand wash 200 ml", Category: "personal-care", Pack: 200, Unit: "ml", MRP: 9900},
	{Name: "Shampoo 180 ml", Category: "hair-care", Pack: 180, Unit: "ml", MRP: 17500},
	{Name: "Antiseptic liquid 250 ml", Category: "health-wellness", Pack: 250, Unit: "ml", MRP: 12500},
	{Name: "Glucose biscuits 250 g", Category: "snacks", Pack: 250, Unit: "g", MRP: 3500},
	// KF fresh produce
	{Name: "Tomato 1 kg", Category: "fruits-vegetables", Pack: 1, Unit: "kg", MRP: 4000},
	{Name: "Onion 1 kg", Category: "fruits-vegetables", Pack: 1, Unit: "kg", MRP: 4500},
	{Name: "Potato 1 kg", Category: "fruits-vegetables", Pack: 1, Unit: "kg", MRP: 4000},
	{Name: "Carrot 500 g", Category: "fruits-vegetables", Pack: 500, Unit: "g", MRP: 3500},
	{Name: "Beans 500 g", Category: "fruits-vegetables", Pack: 500, Unit: "g", MRP: 4000},
	{Name: "Banana robusta 6 pc", Category: "fruits-vegetables", Pack: 6, Unit: "pc", MRP: 5000},
	{Name: "Apple shimla 1 kg", Category: "fruits-vegetables", Pack: 1, Unit: "kg", MRP: 18000},
	{Name: "Cucumber 500 g", Category: "fruits-vegetables", Pack: 500, Unit: "g", MRP: 3000},
	{Name: "Lemon 250 g", Category: "fruits-vegetables", Pack: 250, Unit: "g", MRP: 3000},
	{Name: "Ginger 250 g", Category: "fruits-vegetables", Pack: 250, Unit: "g", MRP: 4000},
	{Name: "Green chilli 100 g", Category: "fruits-vegetables", Pack: 100, Unit: "g", MRP: 1500},
	{Name: "Coriander leaves 100 g", Category: "fruits-vegetables", Pack: 100, Unit: "g", MRP: 1500},
	// NH hardware (no bulb: <seed:asks>)
	{Name: "Claw hammer 500 g", Category: "tools", Pack: 1, Unit: "pc", MRP: 35000},
	{Name: "Screwdriver set 6 pc", Category: "tools", Pack: 6, Unit: "pc", MRP: 30000},
	{Name: "Combination pliers 8 inch", Category: "tools", Pack: 1, Unit: "pc", MRP: 28000},
	{Name: "Measuring tape 5 m", Category: "tools", Pack: 1, Unit: "pc", MRP: 18000},
	{Name: "Wall plug 6 A", Category: "electrical", Pack: 1, Unit: "pc", MRP: 9000},
	{Name: "Switch 6 A", Category: "electrical", Pack: 1, Unit: "pc", MRP: 6000},
	{Name: "Insulation tape 10 m", Category: "electrical", Pack: 1, Unit: "pc", MRP: 2500},
	{Name: "Copper wire 1.5 sq mm 90 m", Category: "electrical", Pack: 1, Unit: "pc", MRP: 180000},
	{Name: "PVC pipe 1 inch 3 m", Category: "plumbing", Pack: 1, Unit: "pc", MRP: 32000},
	{Name: "Ball valve 1 inch", Category: "plumbing", Pack: 1, Unit: "pc", MRP: 22000},
	{Name: "Teflon tape", Category: "plumbing", Pack: 1, Unit: "pc", MRP: 2000},
	{Name: "Wall putty 1 kg", Category: "paints", Pack: 1, Unit: "kg", MRP: 9000},
	{Name: "Paint brush 2 inch", Category: "paints", Pack: 1, Unit: "pc", MRP: 9500},
	{Name: "Enamel paint white 1 L", Category: "paints", Pack: 1, Unit: "l", MRP: 42000},
}

// genGroup generates brand × item × size products in one category.
type genGroup struct {
	cat    string
	brands []string
	items  []genItem
}

// genItem is one kind of product: its sizes ("500 g", "1 kg", "6 pc") and
// its MRP per kg, litre or piece in paise.
type genItem struct {
	name  string
	sizes []string
	per   int64
}

// generated is the master catalogue beyond the fixtures. Brand names are
// used as plain words; the products are made up (no real EAN; barcodes are
// in the 2xx in-store range).
var generated = []genGroup{
	{"staples", []string{"Aashirvaad", "Fortune", "Tata Sampann", "India Gate", "Daawat", "Pillsbury", "Rajdhani", "24 Mantra", "Organic Tattva", "Kohinoor", "Annapoorna", "Patanjali"}, []genItem{
		{"Whole wheat atta", []string{"1 kg", "5 kg", "10 kg"}, 5200},
		{"Basmati rice", []string{"1 kg", "5 kg"}, 14000},
		{"Sona masoori rice", []string{"5 kg", "10 kg"}, 6800},
		{"Toor dal", []string{"500 g", "1 kg"}, 17000},
		{"Moong dal", []string{"500 g", "1 kg"}, 15000},
		{"Chana dal", []string{"500 g", "1 kg"}, 11000},
		{"Urad dal", []string{"500 g", "1 kg"}, 16000},
		{"Masoor dal", []string{"500 g"}, 12000},
		{"Rajma", []string{"500 g"}, 18000},
		{"Kabuli chana", []string{"500 g", "1 kg"}, 16000},
		{"Besan", []string{"500 g", "1 kg"}, 11000},
		{"Sooji", []string{"500 g"}, 7000},
		{"Maida", []string{"500 g", "1 kg"}, 6000},
		{"Poha", []string{"500 g"}, 8000},
		{"Sugar", []string{"1 kg", "5 kg"}, 5500},
		{"Iodised salt", []string{"1 kg"}, 2800},
		{"Ragi flour", []string{"500 g", "1 kg"}, 9000},
		{"Jowar flour", []string{"500 g", "1 kg"}, 9000},
		{"Idli rava", []string{"500 g", "1 kg"}, 7000},
	}},
	{"dairy-eggs", []string{"Amul", "Nandini", "Heritage", "Aavin", "Milky Mist", "Mother Dairy", "Akshayakalpa", "Arokya", "Dodla", "Hatsun"}, []genItem{
		{"Toned milk", []string{"500 ml", "1 l"}, 5000},
		{"Full cream milk", []string{"500 ml", "1 l"}, 6600},
		{"Curd", []string{"200 g", "400 g", "1 kg"}, 9000},
		{"Butter", []string{"100 g", "500 g"}, 56000},
		{"Paneer", []string{"200 g", "500 g"}, 45000},
		{"Ghee", []string{"200 ml", "500 ml", "1 l"}, 62000},
		{"Cheese slices", []string{"100 g", "200 g"}, 60000},
		{"Buttermilk", []string{"200 ml", "500 ml"}, 7500},
		{"Fresh cream", []string{"200 ml"}, 27500},
		{"Sweet lassi", []string{"200 ml"}, 12500},
		{"Flavoured milk", []string{"180 ml", "200 ml"}, 15000},
		{"Probiotic curd", []string{"400 g", "1 kg"}, 12000},
		{"Mozzarella cheese", []string{"200 g"}, 70000},
	}},
	{"fruits-vegetables", []string{"Fresho", "Pro Nature", "Kisan Kart", "Green Basket"}, []genItem{
		{"Tomato", []string{"500 g", "1 kg"}, 4000},
		{"Onion", []string{"1 kg", "2 kg"}, 4500},
		{"Potato", []string{"1 kg", "2 kg"}, 4000},
		{"Carrot", []string{"500 g"}, 7000},
		{"Beans", []string{"250 g", "500 g"}, 8000},
		{"Cabbage", []string{"1 pc"}, 4000},
		{"Cauliflower", []string{"1 pc"}, 5000},
		{"Banana robusta", []string{"6 pc", "12 pc"}, 800},
		{"Apple shimla", []string{"500 g", "1 kg"}, 18000},
		{"Orange", []string{"1 kg"}, 12000},
		{"Pomegranate", []string{"500 g"}, 20000},
		{"Cucumber", []string{"500 g"}, 6000},
		{"Lemon", []string{"250 g"}, 12000},
		{"Ginger", []string{"250 g"}, 16000},
		{"Garlic", []string{"250 g"}, 24000},
		{"Green chilli", []string{"100 g"}, 15000},
		{"Coriander leaves", []string{"100 g"}, 15000},
		{"Spinach", []string{"250 g"}, 8000},
	}},
	{"snacks", []string{"Haldiram's", "Lay's", "Bingo", "Kurkure", "Britannia", "Parle", "Sunfeast", "Bikaji", "Balaji", "Act II", "Too Yumm", "Unibic"}, []genItem{
		{"Aloo bhujia", []string{"200 g", "400 g"}, 50000},
		{"Classic salted chips", []string{"52 g", "90 g"}, 38000},
		{"Masala chips", []string{"52 g", "90 g"}, 38000},
		{"Cream biscuits", []string{"100 g"}, 30000},
		{"Marie biscuits", []string{"250 g"}, 16000},
		{"Digestive biscuits", []string{"250 g"}, 24000},
		{"Butter cookies", []string{"200 g"}, 30000},
		{"Namkeen mixture", []string{"200 g", "400 g"}, 30000},
		{"Instant noodles", []string{"70 g", "280 g"}, 20000},
		{"Popcorn", []string{"70 g"}, 43000},
		{"Salted peanuts", []string{"150 g"}, 33000},
		{"Cake rusk", []string{"200 g"}, 30000},
		{"Khakhra", []string{"200 g", "400 g"}, 45000},
		{"Banana chips", []string{"150 g", "300 g"}, 40000},
		{"Murukku", []string{"200 g", "400 g"}, 35000},
		{"Oat cookies", []string{"150 g", "300 g"}, 50000},
	}},
	{"beverages", []string{"Tata Tea", "Red Label", "Taj Mahal", "Wagh Bakri", "Society", "Lipton"}, []genItem{
		{"Leaf tea", []string{"250 g", "500 g", "1 kg"}, 52000},
		{"Dust tea", []string{"250 g", "500 g"}, 48000},
		{"Green tea bags", []string{"25 pc", "100 pc"}, 700},
		{"Masala chai", []string{"250 g"}, 60000},
	}},
	{"beverages", []string{"Bru", "Nescafe", "Narasu's", "Cothas", "Leo", "Continental"}, []genItem{
		{"Instant coffee", []string{"50 g", "100 g", "200 g"}, 300000},
		{"Filter coffee", []string{"200 g", "500 g"}, 80000},
		{"Coffee chicory blend", []string{"500 g"}, 70000},
	}},
	{"beverages", []string{"Pepsi", "Thums Up", "Sprite", "Fanta", "Limca", "Maaza", "Frooti", "Tropicana", "Real", "Paper Boat", "Bisleri", "Kinley"}, []genItem{
		{"Soft drink", []string{"250 ml", "750 ml", "2 l"}, 5300},
		{"Mixed fruit juice", []string{"200 ml", "1 l"}, 13000},
		{"Mango drink", []string{"600 ml", "1.2 l"}, 7500},
		{"Packaged water", []string{"1 l", "2 l"}, 2000},
		{"Lemon soda", []string{"250 ml", "600 ml"}, 8000},
		{"Energy drink", []string{"250 ml", "500 ml"}, 50000},
	}},
	{"spices-oils", []string{"MDH", "Everest", "Aachi", "Eastern", "Catch", "Sakthi", "MTR", "Badshah"}, []genItem{
		{"Turmeric powder", []string{"100 g", "200 g"}, 40000},
		{"Chilli powder", []string{"100 g", "200 g"}, 45000},
		{"Coriander powder", []string{"100 g", "200 g"}, 30000},
		{"Garam masala", []string{"50 g", "100 g"}, 90000},
		{"Sambar powder", []string{"100 g", "200 g"}, 50000},
		{"Rasam powder", []string{"100 g"}, 50000},
		{"Chicken masala", []string{"50 g", "100 g"}, 90000},
		{"Cumin seeds", []string{"100 g"}, 60000},
		{"Pav bhaji masala", []string{"50 g", "100 g"}, 90000},
		{"Biryani masala", []string{"50 g", "100 g"}, 100000},
		{"Pepper powder", []string{"50 g", "100 g"}, 120000},
		{"Kasuri methi", []string{"25 g", "50 g"}, 150000},
	}},
	{"spices-oils", []string{"Fortune", "Saffola", "Gold Winner", "Dhara", "Gemini", "Idhayam", "Parachute", "KLF"}, []genItem{
		{"Sunflower oil", []string{"1 l", "5 l"}, 15000},
		{"Groundnut oil", []string{"1 l"}, 22000},
		{"Rice bran oil", []string{"1 l", "5 l"}, 16000},
		{"Mustard oil", []string{"1 l"}, 19000},
		{"Sesame oil", []string{"500 ml", "1 l"}, 36000},
		{"Coconut oil", []string{"500 ml", "1 l"}, 28000},
	}},
	{"bakery", []string{"Modern", "Britannia", "Harvest Gold", "English Oven", "Bonn", "Daily Bread"}, []genItem{
		{"White bread", []string{"400 g", "800 g"}, 12500},
		{"Brown bread", []string{"400 g"}, 15000},
		{"Multigrain bread", []string{"400 g"}, 17500},
		{"Milk rusk", []string{"300 g"}, 20000},
		{"Fruit cake", []string{"150 g"}, 40000},
		{"Pav", []string{"6 pc"}, 700},
		{"Burger buns", []string{"4 pc"}, 1200},
	}},
	{"household", []string{"Surf Excel", "Ariel", "Tide", "Rin", "Wheel", "Vim", "Pril", "Lizol", "Harpic", "Colin", "Odonil", "Good Knight"}, []genItem{
		{"Detergent powder", []string{"500 g", "1 kg", "2 kg"}, 14000},
		{"Liquid detergent", []string{"500 ml", "1 l"}, 25000},
		{"Detergent bar", []string{"250 g"}, 12000},
		{"Dishwash bar", []string{"200 g"}, 12500},
		{"Dishwash gel", []string{"250 ml", "750 ml"}, 26000},
		{"Floor cleaner", []string{"500 ml", "1 l"}, 20000},
		{"Toilet cleaner", []string{"500 ml"}, 22000},
		{"Glass cleaner", []string{"500 ml"}, 21000},
		{"Fabric conditioner", []string{"400 ml", "860 ml"}, 30000},
		{"Garbage bags medium", []string{"30 pc", "60 pc"}, 400},
		{"Mosquito repellent refill", []string{"1 pc", "2 pc"}, 8000},
	}},
	{"personal-care", []string{"Dove", "Lux", "Lifebuoy", "Dettol", "Santoor", "Medimix", "Cinthol", "Pears", "Mysore Sandal", "Hamam", "Colgate", "Pepsodent", "Close Up", "Sensodyne"}, []genItem{
		{"Bath soap", []string{"100 g", "4 pc"}, 45000},
		{"Body wash", []string{"250 ml"}, 80000},
		{"Hand wash", []string{"200 ml", "750 ml"}, 50000},
		{"Toothpaste", []string{"100 g", "150 g"}, 70000},
		{"Toothbrush soft", []string{"1 pc", "4 pc"}, 4000},
		{"Talcum powder", []string{"100 g"}, 80000},
		{"Shaving cream", []string{"70 g", "100 g"}, 90000},
		{"Deodorant spray", []string{"150 ml", "220 ml"}, 150000},
		{"Mouthwash", []string{"250 ml", "500 ml"}, 60000},
	}},
	{"medicines", []string{"Cipla", "Sun Pharma", "Dr. Reddy's", "Mankind", "Himalaya", "Dabur"}, []genItem{
		{"Paracetamol 500 tablets", []string{"15 pc"}, 200},
		{"Cetirizine tablets", []string{"10 pc"}, 250},
		{"Antacid tablets", []string{"10 pc"}, 300},
		{"Cough syrup", []string{"100 ml"}, 120000},
		{"Pain relief balm", []string{"25 g"}, 400000},
		{"ORS sachet", []string{"1 pack", "5 pack"}, 2200},
		{"Vitamin C tablets", []string{"15 pc"}, 1500},
	}},
	{"health-wellness", []string{"Dabur", "Himalaya", "Patanjali", "Baidyanath", "Zandu", "Organic India"}, []genItem{
		{"Chyawanprash", []string{"500 g", "1 kg"}, 50000},
		{"Honey", []string{"250 g", "500 g"}, 70000},
		{"Ashwagandha tablets", []string{"60 pc"}, 300},
		{"Tulsi drops", []string{"30 ml"}, 600000},
		{"Digestive churna", []string{"100 g"}, 120000},
	}},
	{"baby-care", []string{"Johnson's", "Himalaya Baby", "Pampers", "Huggies", "MamyPoko", "Mamaearth", "Sebamed"}, []genItem{
		{"Baby soap", []string{"75 g"}, 80000},
		{"Baby oil", []string{"100 ml", "200 ml"}, 150000},
		{"Baby powder", []string{"100 g"}, 120000},
		{"Baby wipes", []string{"72 pc"}, 300},
		{"Diaper pants M", []string{"20 pc", "56 pc"}, 1500},
		{"Diaper pants L", []string{"18 pc", "48 pc"}, 1700},
	}},
	{"skin-care", []string{"Nivea", "Ponds", "Lakme", "Mamaearth", "Himalaya", "Garnier", "Biotique", "Lotus", "Vaseline", "Cetaphil"}, []genItem{
		{"Face wash", []string{"50 ml", "100 ml"}, 250000},
		{"Moisturising cream", []string{"50 g", "100 g"}, 300000},
		{"Body lotion", []string{"200 ml", "400 ml"}, 120000},
		{"Sunscreen SPF 50", []string{"50 g"}, 600000},
		{"Cold cream", []string{"100 ml"}, 200000},
		{"Lip balm", []string{"4.5 g", "10 g"}, 3000000},
		{"Face scrub", []string{"50 g", "100 g"}, 250000},
	}},
	{"hair-care", []string{"Clinic Plus", "Head & Shoulders", "Pantene", "Sunsilk", "Dove", "TRESemme", "Parachute", "Indulekha", "Bajaj", "Dabur Amla"}, []genItem{
		{"Shampoo", []string{"80 ml", "180 ml", "340 ml"}, 95000},
		{"Conditioner", []string{"80 ml", "180 ml"}, 110000},
		{"Hair oil", []string{"100 ml", "200 ml"}, 90000},
		{"Hair serum", []string{"50 ml"}, 500000},
		{"Anti-dandruff shampoo", []string{"180 ml", "340 ml"}, 110000},
		{"Hair mask", []string{"200 ml", "500 ml"}, 120000},
	}},
	{"makeup", []string{"Lakme", "Maybelline", "Sugar", "Elle 18", "Blue Heaven", "Swiss Beauty"}, []genItem{
		{"Kajal", []string{"1 pc"}, 25000},
		{"Lipstick", []string{"1 pc"}, 40000},
		{"Nail polish", []string{"1 pc"}, 15000},
		{"Compact powder", []string{"9 g"}, 3000000},
		{"Eyeliner", []string{"1 pc"}, 30000},
	}},
	{"mobile-accessories", []string{"boAt", "Portronics", "Ambrane", "Mi", "Realme", "Zebronics", "pTron", "Syska"}, []genItem{
		{"USB-C cable 1 m", []string{"1 pc"}, 29900},
		{"Micro USB cable 1 m", []string{"1 pc"}, 19900},
		{"Fast charger 20 W", []string{"1 pc"}, 79900},
		{"Power bank 10000 mAh", []string{"1 pc"}, 149900},
		{"Wired earphones", []string{"1 pc"}, 49900},
		{"Phone stand", []string{"1 pc"}, 29900},
	}},
	{"batteries-lighting", []string{"Duracell", "Eveready", "Panasonic", "Nippo", "Philips", "Wipro", "Havells", "Syska", "Bajaj"}, []genItem{
		{"AA batteries", []string{"2 pc", "4 pc"}, 4500},
		{"AAA batteries", []string{"2 pc", "4 pc"}, 4500},
		{"LED bulb 12 W", []string{"1 pc"}, 15000},
		{"LED tube light 20 W", []string{"1 pc"}, 45000},
		{"Emergency torch", []string{"1 pc"}, 60000},
	}},
	{"small-appliances", []string{"Prestige", "Pigeon", "Bajaj", "Philips", "Havells", "Butterfly", "Morphy Richards"}, []genItem{
		{"Electric kettle 1.5 l", []string{"1 pc"}, 120000},
		{"Dry iron", []string{"1 pc"}, 90000},
		{"Mixer grinder 500 W", []string{"1 pc"}, 350000},
		{"Induction cooktop", []string{"1 pc"}, 280000},
		{"Hand blender", []string{"1 pc"}, 150000},
	}},
	{"school-supplies", []string{"Classmate", "Navneet", "Camlin", "Faber-Castell", "Doms", "Apsara", "Natraj", "Cello"}, []genItem{
		{"Long notebook 172 pages", []string{"1 pc", "6 pc"}, 6000},
		{"Pencils", []string{"10 pc"}, 500},
		{"Ball pens blue", []string{"5 pc", "10 pc"}, 1000},
		{"Crayons", []string{"12 pc", "24 pc"}, 500},
		{"Geometry box", []string{"1 pc"}, 15000},
		{"Eraser", []string{"5 pc"}, 500},
	}},
	{"office-supplies", []string{"Kangaro", "Fevicol", "JK Copier", "Solo", "Oddy", "Stic"}, []genItem{
		{"Stapler", []string{"1 pc"}, 15000},
		{"Stapler pins", []string{"1 pack", "5 pack"}, 2000},
		{"A4 paper 500 sheets", []string{"1 pack"}, 35000},
		{"Glue stick", []string{"15 g"}, 400000},
		{"File folder", []string{"5 pc"}, 4000},
		{"Sticky notes", []string{"1 pack"}, 9000},
	}},
	{"tools", []string{"Stanley", "Taparia", "Bosch", "Black+Decker", "Pye", "Jhalani"}, []genItem{
		{"Claw hammer", []string{"1 pc"}, 40000},
		{"Screwdriver set", []string{"6 pc"}, 6000},
		{"Adjustable wrench", []string{"1 pc"}, 45000},
		{"Combination pliers", []string{"1 pc"}, 30000},
		{"Hacksaw frame", []string{"1 pc"}, 35000},
		{"Measuring tape 5 m", []string{"1 pc"}, 20000},
		{"Allen key set", []string{"9 pc"}, 4000},
	}},
	{"electrical", []string{"Anchor", "Havells", "Legrand", "Polycab", "Finolex", "GM", "Philips", "Crompton"}, []genItem{
		{"Modular switch 6 A", []string{"1 pc"}, 7000},
		{"Modular socket 16 A", []string{"1 pc"}, 15000},
		{"Spike guard 4 socket", []string{"1 pc"}, 60000},
		{"Copper wire 1.5 sq mm", []string{"1 pc"}, 190000},
		{"MCB 32 A", []string{"1 pc"}, 35000},
		{"Ceiling fan regulator", []string{"1 pc"}, 40000},
	}},
	{"plumbing", []string{"Supreme", "Ashirvad", "Astral", "Prince", "Finolex Pipes", "Jaquar"}, []genItem{
		{"PVC pipe 1 inch 3 m", []string{"1 pc"}, 30000},
		{"Ball valve 1 inch", []string{"1 pc"}, 25000},
		{"Bib tap", []string{"1 pc"}, 45000},
		{"Shower head", []string{"1 pc"}, 80000},
		{"Solvent cement", []string{"100 ml"}, 150000},
	}},
	{"paints", []string{"Asian Paints", "Berger", "Nerolac", "Dulux", "Indigo", "Nippon"}, []genItem{
		{"Enamel paint white", []string{"500 ml", "1 l"}, 40000},
		{"Interior emulsion", []string{"1 l", "4 l"}, 35000},
		{"Primer", []string{"1 l"}, 30000},
		{"Wall putty", []string{"1 kg", "5 kg"}, 8000},
		{"Paint brush 2 inch", []string{"1 pc"}, 10000},
	}},
}

// parseSize reads "500 g", "1.2 l", "6 pc", "1 pack" into a pack size, a
// unit and the size in kg, litres or pieces (for the price).
func parseSize(s string) (pack float64, unit string, base float64) {
	f := strings.Fields(s)
	if len(f) != 2 {
		panic("seed: bad size " + s)
	}
	n, err := strconv.ParseFloat(f[0], 64)
	if err != nil {
		panic("seed: bad size " + s)
	}
	switch f[1] {
	case "g", "ml":
		return n, f[1], n / 1000
	case "kg", "l", "pc", "pack":
		return n, f[1], n
	}
	panic("seed: bad unit " + s)
}

// ean13 is a barcode in the 2xx in-store range with its check digit, so it
// never matches a real product.
func ean13(n int) string {
	s := fmt.Sprintf("29%010d", n)
	sum := 0
	for i, c := range s {
		d := int(c - '0')
		if i%2 == 1 {
			d *= 3
		}
		sum += d
	}
	return s + strconv.Itoa((10-sum%10)%10)
}

// masterProducts returns every seeded product with its fixed id: the
// fixtures as numbers 1…, the generated ones from 1000 on (in list order,
// skipping a name a fixture or an earlier group already took).
func masterProducts() ([]seedProduct, []ids.ID) {
	out := append([]seedProduct(nil), fixtureProducts...)
	pids := make([]ids.ID, 0, 3200)
	seen := map[string]bool{}
	for i, p := range fixtureProducts {
		pids = append(pids, seqID(productIDPrefix, i+1))
		seen[strings.ToLower(p.Name)] = true
	}
	n := 1000
	for _, g := range generated {
		for _, it := range g.items {
			for _, b := range g.brands {
				for _, sz := range it.sizes {
					name := b + " " + it.name + " " + sz
					if seen[strings.ToLower(name)] {
						continue
					}
					seen[strings.ToLower(name)] = true
					pack, unit, base := parseSize(sz)
					mrp := int64(math.Round(float64(it.per)*base/100)) * 100 // whole rupees
					// A small brand spread so brands do not all cost the same.
					mrp += int64(len(b)%5) * 100
					if mrp < 500 {
						mrp = 500
					}
					p := seedProduct{Name: name, Brand: b, Category: g.cat, Pack: pack, Unit: unit, MRP: mrp}
					if n%3 != 0 {
						p.Barcode = ean13(n)
					}
					out = append(out, p)
					pids = append(pids, seqID(productIDPrefix, n))
					n++
				}
			}
		}
	}
	return out, pids
}

func slugOf(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

// productSeedID returns a fixture product's fixed id by its name; it panics
// on an unknown name (a seed bug the seed test finds).
func productSeedID(name string) ids.ID {
	for i, p := range fixtureProducts {
		if p.Name == name {
			return seqID(productIDPrefix, i+1)
		}
	}
	panic("seed: no product " + name)
}

// --- store items ----------------------------------------------------------------

// stockIn is one stock in of a seeded item: Ago before the seed clock, with
// an expiry (days from today, IST) or a cost, which start a batch.
type stockIn struct {
	Ago     time.Duration
	Qty     int32
	Expires *int
	Cost    *int64
}

// seedItem is one store item: the book's short name (I1, J7, …; "" for the
// rest), the product, price and store MRP (0: none) and its stock ins. With
// no stock ins the item has no stock and no stock_updated_at.
type seedItem struct {
	Key     string
	Product string
	Price   int64
	MRP     int64
	Stock   []stockIn
}

func days(n int) *int      { return &n }
func paise(n int64) *int64 { return &n }

// fresh is "at the seed clock": stock_updated_at is the clock time.
func fresh(q int32) []stockIn { return []stockIn{{Qty: q}} }

// seedStoreItems lists each live store's items in a fixed order; the order
// fixes the item ids (storeNo × 100 + position). Append only.
var seedStoreItems = []struct {
	Store string
	No    int
	Items []seedItem
}{
	{"SL", 1, []seedItem{
		{Key: "I1", Product: "Amul Butter 100 g", Price: 5800, MRP: 6000, Stock: fresh(10)},
		{Key: "I2", Product: "Maggi 2-Minute Noodles 70 g", Price: 1400, MRP: 1500, Stock: fresh(50)},
		{Key: "I3", Product: "Nandini Toned Milk 500 ml", Price: 2400, MRP: 2400, Stock: fresh(30)},
		{Key: "I4", Product: "Wheat atta 5 kg", Price: 24500, MRP: 26000, Stock: fresh(6)},
		{Key: "I5", Product: "Coca-Cola 750 ml", Price: 4000, MRP: 4000, Stock: fresh(12)},
		{Key: "I6", Product: "Dolo 650 strip", Price: 3000, MRP: 3200, Stock: fresh(1)},
		// Stale: stock last updated 84 h ago (stock_stale_after_hours is 72); latest batch cost ₹128.
		{Key: "I7", Product: "Toor dal 1 kg", Price: 16000, MRP: 17000, Stock: []stockIn{{Ago: 84 * time.Hour, Qty: 8, Cost: paise(12800)}}},
		// 10 loose and one batch of 10 expiring in 3 days.
		{Key: "I8", Product: "Curd 400 g", Price: 3500, MRP: 3500, Stock: []stockIn{{Qty: 10}, {Qty: 10, Expires: days(3)}}},
		{Key: "I9", Product: "QA pack ₹99.99", Price: 9999, MRP: 9999, Stock: fresh(50)},
		{Key: "I10", Product: "QA pack ₹100.00", Price: 10000, MRP: 10000, Stock: fresh(50)},
		{Key: "I11", Product: "QA pack ₹100.01", Price: 10001, MRP: 10001, Stock: fresh(50)},
		{Key: "I12", Product: "QA pack ₹0.50", Price: 50, MRP: 50, Stock: fresh(100)},
		{Key: "I13", Product: "QA pack ₹120", Price: 12000, MRP: 12000, Stock: fresh(50)},
		{Key: "I14", Product: "QA pack ₹487", Price: 48700, MRP: 48700, Stock: fresh(20)},
	}},
	{"SL2", 2, []seedItem{
		{Key: "J1", Product: "Tata Tea Gold 250 g", Price: 14500, Stock: fresh(12)},
		{Key: "J2", Product: "Curd 200 g", Price: 2000, Stock: []stockIn{{Qty: 12, Expires: days(1)}}},
		{Key: "J3", Product: "Buttermilk 200 ml", Price: 1500, Stock: []stockIn{{Qty: 10, Expires: days(2)}}},
		{Key: "J4", Product: "Cheese slices 200 g", Price: 12000, Stock: []stockIn{{Qty: 8, Expires: days(3)}}},
		{Key: "J5", Product: "Fresh cream 200 ml", Price: 5500, Stock: []stockIn{{Qty: 6, Expires: days(4)}}},
		// 6 expired yesterday (received 8 days ago), 4 expire in 6 days.
		{Key: "J6", Product: "Paneer 200 g", Price: 9000, Stock: []stockIn{{Ago: 8 * day, Qty: 6, Expires: days(-1)}, {Qty: 4, Expires: days(6)}}},
		// The older batch costs ₹150, the latest ₹146 (above the ₹140 price).
		{Key: "J7", Product: "Sunflower oil 1 L", Price: 14000, MRP: 15500, Stock: []stockIn{{Ago: 10 * day, Qty: 8, Cost: paise(15000)}, {Qty: 12, Cost: paise(14600)}}},
		{Key: "J8", Product: "Mango pickle 500 g", Price: 6500, Stock: []stockIn{{Qty: 12, Cost: paise(4000)}}},
		{Key: "J9", Product: "Agarbatti pack", Price: 3000, Stock: fresh(5)},
		{Key: "J10", Product: "Papad 200 g", Price: 4500, Stock: []stockIn{{Qty: 9, Cost: paise(3000)}}},
		{Key: "J11", Product: "Idli rice 1 kg", Price: 7000},
		{Key: "J12", Product: "Ragi flour 500 g", Price: 4500, Stock: []stockIn{{Ago: 9 * day, Qty: 10}}},
		{Key: "J13", Product: "Poha 500 g", Price: 3800, Stock: []stockIn{{Ago: 9 * day, Qty: 10}}},
		{Key: "J14", Product: "Jaggery 500 g", Price: 6000, Stock: []stockIn{{Ago: 9 * day, Qty: 10}}},
		{Key: "J15", Product: "Sabudana 500 g", Price: 5500, Stock: []stockIn{{Ago: 9 * day, Qty: 10}}},
		{Key: "J16", Product: "Vermicelli 400 g", Price: 4000, Stock: []stockIn{{Ago: 9 * day, Qty: 10}}},
		{Key: "J17", Product: "Rice flour 1 kg", Price: 6000, Stock: []stockIn{{Ago: 9 * day, Qty: 10}}},
		{Key: "J18", Product: "Bread 400 g", Price: 4500, Stock: fresh(60)},
		{Key: "J19", Product: "Eggs tray 12", Price: 8400, Stock: fresh(50)},
		{Key: "J20", Product: "Nandini Toned Milk 500 ml", Price: 2400, Stock: fresh(120)},
		{Key: "J21", Product: "Parle-G", Price: 1000, Stock: fresh(80)},
		// A case price entered as the unit cost (₹240 against an MRP of ₹28).
		{Key: "J22", Product: "Salt 1 kg", Price: 2400, MRP: 2800, Stock: []stockIn{{Qty: 30, Cost: paise(24000)}}},
	}},
	{"AP", 3, append([]seedItem{
		{Product: "Boost 500 g", Price: 27000, Stock: fresh(8)},
		// <seed:asks>: listed only here, ₹110 against an MRP of ₹120, 2 on hand, last confirmed 5 days ago.
		{Product: "LED bulb 9 W", Price: 11000, MRP: 12000, Stock: []stockIn{{Ago: 5 * day, Qty: 2}}},
	}, pool(3, 18, "Sona masoori rice 5 kg", "Moong dal 1 kg", "Chana dal 1 kg", "Groundnut oil 1 L",
		"Turmeric powder 100 g", "Chilli powder 100 g", "Filter coffee powder 200 g", "Rusk 300 g", "Bath soap 100 g",
		"Toothpaste 150 g", "Detergent powder 1 kg", "Dishwash bar 200 g", "Cream biscuits 100 g", "Potato chips 52 g",
		"Coconut oil 500 ml", "Mustard seeds 100 g", "Tamarind 250 g", "Matchbox pack of 10")...)},
	{"NH", 4, append([]seedItem{
		{Product: "Extension board 4 socket", Price: 42000, Stock: fresh(6)},
	}, pool(4, 14, "Claw hammer 500 g", "Screwdriver set 6 pc", "Combination pliers 8 inch", "Measuring tape 5 m",
		"Wall plug 6 A", "Switch 6 A", "Insulation tape 10 m", "Copper wire 1.5 sq mm 90 m", "PVC pipe 1 inch 3 m",
		"Ball valve 1 inch", "Teflon tape", "Wall putty 1 kg", "Paint brush 2 inch", "Enamel paint white 1 L")...)},
	{"KF", 5, pool(5, 12, "Tomato 1 kg", "Onion 1 kg", "Potato 1 kg", "Carrot 500 g", "Beans 500 g",
		"Banana robusta 6 pc", "Apple shimla 1 kg", "Cucumber 500 g", "Lemon 250 g", "Ginger 250 g",
		"Green chilli 100 g", "Coriander leaves 100 g")},
	{"AM", 6, pool(6, 10, "Sona masoori rice 5 kg", "Toor dal 1 kg", "Sunflower oil 1 L", "Sugar 1 kg",
		"Bread 400 g", "Eggs tray 12", "Detergent powder 1 kg", "Bath soap 100 g", "Potato chips 52 g", "Glucose biscuits 250 g")},
	{"HY", 7, pool(7, 15, "Sona masoori rice 5 kg", "Toor dal 1 kg", "Moong dal 1 kg", "Sunflower oil 1 L",
		"Groundnut oil 1 L", "Sugar 1 kg", "Salt 1 kg", "Tamarind 250 g", "Chilli powder 100 g", "Turmeric powder 100 g",
		"Bread 400 g", "Eggs tray 12", "Toothpaste 150 g", "Shampoo 180 ml", "Hand wash 200 ml")},
	// <seed:price-compare>: fresh stock, no sales history.
	{"MM", 8, []seedItem{
		{Product: "Sugar 1 kg", Price: 6200, Stock: fresh(40)},
		{Product: "Jaggery 1 kg", Price: 9500, Stock: fresh(25)},
	}},
}

// Sugar 1 kg at CH01–CH12 (median ₹58) and Jaggery 1 kg at CH01–CH09
// (median ₹90, nine stores, under the 10-store floor of D-71).
var (
	chennaiSugar   = []int64{5200, 5400, 5500, 5600, 5700, 5800, 5800, 5900, 6000, 6100, 6200, 6400}
	chennaiJaggery = []int64{8400, 8600, 8800, 8900, 9000, 9100, 9200, 9400, 9600}
)

func init() {
	for i := range chennaiNeighbours {
		items := []seedItem{{Product: "Sugar 1 kg", Price: chennaiSugar[i], Stock: fresh(int32(20 + i))}}
		if i < len(chennaiJaggery) {
			items = append(items, seedItem{Product: "Jaggery 1 kg", Price: chennaiJaggery[i], Stock: fresh(int32(10 + i))})
		}
		seedStoreItems = append(seedStoreItems, struct {
			Store string
			No    int
			Items []seedItem
		}{fmt.Sprintf("CH%02d", i+1), 9 + i, items})
	}
}

// pool lists n everyday items for store no: the price a little under the
// product's MRP, stock between 6 and 45, and every third item with a dated
// batch well away from any insight window (KF's produce 8 to 11 days, the
// rest 60 days and more), no cost.
func pool(no, n int, names ...string) []seedItem {
	if len(names) != n {
		panic(fmt.Sprintf("seed: store %d: %d pool items, want %d", no, len(names), n))
	}
	out := make([]seedItem, 0, n)
	for i, name := range names {
		mrp := fixtureMRP(name)
		price := mrp - int64((no+i)%3)*100
		if price <= 0 {
			price = mrp
		}
		qty := int32(6 + (no*7+i*11)%40) //nolint:gosec // small
		st := stockIn{Qty: qty}
		if i%3 == 0 {
			d := 60 + i
			if no == 5 {
				d = 8 + i%4
			}
			st.Expires = days(d)
		}
		out = append(out, seedItem{Product: name, Price: price, Stock: []stockIn{st}})
	}
	return out
}

func fixtureMRP(name string) int64 {
	for _, p := range fixtureProducts {
		if p.Name == name {
			return p.MRP
		}
	}
	panic("seed: no product " + name)
}

// storeItemSeedID returns the fixed id of the book's item I1–I14 or J1–J22.
func storeItemSeedID(key string) ids.ID {
	for _, s := range seedStoreItems {
		for i, it := range s.Items {
			if it.Key == key {
				return seqID(storeItemIDPrefix, s.No*100+i+1)
			}
		}
	}
	panic("seed: no store item " + key)
}

// seedIDs is a deterministic id source for the stock writer: the time part
// is the movement's time, the random part a fixed tag and a counter.
func seedIDs() func(time.Time) ids.ID {
	var n uint64
	return func(t time.Time) ids.ID {
		n++
		var u ulid.ULID
		if err := u.SetTime(ulid.Timestamp(t)); err != nil {
			panic("seed: id time: " + err.Error())
		}
		e := make([]byte, 10)
		e[0], e[1] = 0x5e, 0xed
		binary.BigEndian.PutUint64(e[2:], n)
		if err := u.SetEntropy(e); err != nil {
			panic("seed: id entropy: " + err.Error())
		}
		return u
	}
}

// seedcatalogue writes brands, master products, store items and their stock.
func seedcatalogue(ctx context.Context, d Deps, _ Profile) error {
	now := d.Clock.Now().UTC()
	q := d.DB.Q(ctx)
	products, pids := masterProducts()

	// Brands, ids by sorted slug.
	slugs := map[string]string{}
	for _, p := range products {
		if p.Brand != "" {
			slugs[slugOf(p.Brand)] = p.Brand
		}
	}
	keys := make([]string, 0, len(slugs))
	for k := range slugs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	brandID := map[string]pgtype.UUID{}
	bIDs := make([]pgtype.UUID, 0, len(keys))
	bNames := make([]string, 0, len(keys))
	for i, k := range keys {
		id := ids.UUID(seqID(brandIDPrefix, i+1))
		brandID[slugs[k]] = id
		bIDs = append(bIDs, id)
		bNames = append(bNames, slugs[k])
	}
	if _, err := q.Exec(ctx, `INSERT INTO brands (id, name, slug, created_at, updated_at)
		SELECT u.id, u.name, u.slug, $4, $4 FROM unnest($1::uuid[], $2::text[], $3::text[]) AS u(id, name, slug)
		ON CONFLICT DO NOTHING`, bIDs, bNames, keys, now); err != nil {
		return fmt.Errorf("brands: %w", err)
	}
	// A brand already there under its slug keeps its own id: read the ids back
	// so products point at the row that exists.
	rows, err := q.Query(ctx, `SELECT id, slug FROM brands WHERE slug = ANY($1::text[])`, keys)
	if err != nil {
		return fmt.Errorf("brand ids: %w", err)
	}
	for rows.Next() {
		var id pgtype.UUID
		var slug string
		if err := rows.Scan(&id, &slug); err != nil {
			rows.Close()
			return fmt.Errorf("brand ids: %w", err)
		}
		brandID[slugs[slug]] = id
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("brand ids: %w", err)
	}

	// Products in one statement.
	n := len(products)
	pid := make([]pgtype.UUID, n)
	names := make([]string, n)
	brands := make([]pgtype.UUID, n)
	cats := make([]string, n)
	packs := make([]float64, n)
	units := make([]string, n)
	mrps := make([]*int64, n)
	codes := make([]*string, n)
	for i, p := range products {
		pid[i] = ids.UUID(pids[i])
		names[i], cats[i], packs[i], units[i] = p.Name, categoryID(p.Category), p.Pack, p.Unit
		if p.Brand != "" {
			brands[i] = brandID[p.Brand]
		}
		if p.MRP > 0 {
			mrps[i] = paise(p.MRP)
		}
		if p.Barcode != "" {
			codes[i] = &p.Barcode
		}
	}
	if _, err := q.Exec(ctx, `INSERT INTO products (id, name, brand_id, category_id, pack_size, unit, mrp_paise,
			barcodes, status, created_at, updated_at)
		SELECT u.id, u.name, u.brand_id, u.category_id::uuid, u.pack_size::numeric(10,3), u.unit, u.mrp,
			CASE WHEN u.code IS NULL THEN '{}'::text[] ELSE ARRAY[u.code] END, 'active', $9, $9
		FROM unnest($1::uuid[], $2::text[], $3::uuid[], $4::text[], $5::float8[], $6::text[], $7::bigint[], $8::text[])
			AS u(id, name, brand_id, category_id, pack_size, unit, mrp, code)
		ON CONFLICT DO NOTHING`, pid, names, brands, cats, packs, units, mrps, codes, now); err != nil {
		return fmt.Errorf("products: %w", err)
	}

	// Store items and their stock, through the stock writer.
	next := seedIDs()
	today := catalogue.Today(now)
	created := now.Add(-30 * day)
	for _, s := range seedStoreItems {
		storeID := storeSeedID(s.Store)
		for i, it := range s.Items {
			itemID := seqID(storeItemIDPrefix, s.No*100+i+1)
			var mrp *int64
			if it.MRP > 0 {
				mrp = paise(it.MRP)
			}
			var got pgtype.UUID
			err := q.QueryRow(ctx, `INSERT INTO store_items (id, store_id, product_id, price_paise, mrp_paise, is_listed,
					search, created_at, updated_at)
				SELECT $1, $2, p.id, $4, $5, true,
					p.search || setweight(to_tsvector('simple'::regconfig, coalesce(b.name, '')), 'B'), $6, $6
				FROM products p LEFT JOIN brands b ON b.id = p.brand_id WHERE p.id = $3
				ON CONFLICT DO NOTHING RETURNING id`,
				ids.UUID(itemID), ids.UUID(storeID), ids.UUID(productSeedID(it.Product)), it.Price, mrp, created).Scan(&got)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					continue // seeded by an earlier run: leave its stock alone
				}
				return fmt.Errorf("store item %s/%s: %w", s.Store, it.Product, err)
			}
			for j, in := range it.Stock {
				w := catalogue.NewWriterWithIDs(clock.NewFake(now.Add(-in.Ago)), next)
				src := catalogue.SourceOpening
				if j > 0 {
					src = catalogue.SourceRestock
				}
				var exp *time.Time
				if in.Expires != nil {
					e := today.AddDate(0, 0, *in.Expires)
					exp = &e
				}
				if _, err := w.AddStock(ctx, q, catalogue.StockIn{
					StoreID: storeID, ItemID: itemID, Qty: in.Qty, Source: src, ExpiresOn: exp, CostPaise: in.Cost,
				}); err != nil {
					return fmt.Errorf("stock %s/%s: %w", s.Store, it.Product, err)
				}
			}
		}
	}
	return nil
}
