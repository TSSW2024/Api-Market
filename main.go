package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/gocolly/colly/v2"
	"github.com/gocolly/colly/v2/extensions"
)

type CryptoInfo struct {
	Index     string `json:"index"`
	Image     string `json:"image"`
	Name      string `json:"name"`
	Price     string `json:"price"`
	Change24h string `json:"change_24h"`
}

type BinancePriceInfo struct {
	Symbol string `json:"symbol"`
	Price  string `json:"price"`
}

type BinanceTickerInfo struct {
	Symbol             string `json:"symbol"`
	PriceChangePercent string `json:"priceChangePercent"`
}

func main() {
	// Configurar Gin como el enrutador
	router := gin.Default()

	// Configurar CORS
	router.Use(corsMiddleware())

	// Definir la ruta
	router.GET("/", handleRequest)

	// Servir archivos estáticos (imágenes) desde la carpeta local
	router.Static("/images", "./images")

	// Iniciar el servidor en el puerto 8080
	fmt.Println("Servidor escuchando en http://localhost:8080")
	log.Fatal(router.Run(":8080"))
}

func handleRequest(c *gin.Context) {
	// URL a scrapear
	url := "https://www.binance.com/es/markets/trading_data/rankings"

	// Crear un nuevo colector Colly con extensión para manejar cookies
	collyCollector := colly.NewCollector(
		colly.AllowedDomains("www.binance.com"),
	)
	extensions.RandomUserAgent(collyCollector)

	// Variables para almacenar los resultados
	var allResults []CryptoInfo
	var groupedResults [][]CryptoInfo
	var mutex sync.Mutex

	// Obtener los precios y cambios de la API de Binance
	prices, changes := getPricesAndChanges()

	// Función para extraer la información de cada bloque relevante
	collyCollector.OnHTML("div.rounded-xl.border.border-line.p-m", func(e *colly.HTMLElement) {
		e.ForEach("div.css-1qyk0y6", func(index int, div *colly.HTMLElement) {
			info := CryptoInfo{}

			// Obtener el índice del div css-1ycllpv
			info.Index = div.ChildText("div.css-1ycllpv")

			img := div.ChildAttr("div.subtitle4.line-clamp-1.truncate.css-whts0r img", "src")
			info.Image = img

			name := div.ChildText("div.css-lzd0h4")
			info.Name = name

			// Verificar si el nombre está vacío antes de continuar
			if info.Name != "" {
				symbol := strings.ToUpper(info.Name) + "USDT"
				info.Price = prices[symbol]
				info.Change24h = (changes[symbol])

				// Descargar la imagen si la URL está presente
				if info.Image != "" {
					imageFilename := strings.ReplaceAll(info.Name, " ", "_") + ".jpg"
					if !imageExists(imageFilename) {
						downloadImage(info.Image, imageFilename)
					}
					// Obtener el esquema (http o https) de la solicitud actual
					scheme := "http"
					if c.Request.TLS != nil {
						scheme = "https"
					}
					// Reemplazar la URL de la imagen con la ruta local
					info.Image = scheme + "://" + c.Request.Host + "/images/" + imageFilename
				}

				mutex.Lock()
				allResults = append(allResults, info)
				mutex.Unlock()
			}
		})
	})

	// Manejar errores de solicitud
	collyCollector.OnError(func(r *colly.Response, err error) {
		log.Printf("Request URL: %s failed with response: %v\nError: %s\n", r.Request.URL, r, err)
	})

	// Visitar la URL y ejecutar el scraping cada vez que se realiza una solicitud
	err := collyCollector.Visit(url)
	if err != nil {
		log.Fatal(err)
	}

	// Dividir los resultados en grupos de 10 objetos y ordenarlos según las categorías
	groupedResults = categorizeResults(allResults)

	// Mapa para almacenar los resultados agrupados por categoría
	categorizedResults := map[string][]CryptoInfo{
		"Populares":    groupedResults[0],
		"Ganadores":    groupedResults[1],
		"Perdedores":   groupedResults[2],
		"MayorVolumen": groupedResults[3],
	}

	// Devolver los resultados como JSON
	c.JSON(http.StatusOK, categorizedResults)
}

// Función para dividir y agrupar los resultados en grupos de 10 objetos
func categorizeResults(allResults []CryptoInfo) [][]CryptoInfo {
	var groupedResults [][]CryptoInfo
	for i := 0; i < len(allResults); i += 10 {
		end := i + 10
		if end > len(allResults) {
			end = len(allResults)
		}
		groupedResults = append(groupedResults, allResults[i:end])
	}
	return groupedResults
}

// Middleware para configurar CORS
func corsMiddleware() gin.HandlerFunc {
	config := cors.DefaultConfig()
	config.AllowOrigins = []string{"*"}
	config.AllowHeaders = []string{"Origin", "Content-Length", "Content-Type"}
	config.AllowMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}

	return cors.New(config)
}

// Función para verificar si una imagen ya existe localmente
func imageExists(filename string) bool {
	_, err := os.Stat("images/" + filename)
	return !os.IsNotExist(err)
}

// Función para descargar la imagen desde la URL y almacenarla localmente
func downloadImage(url, filename string) {
	response, err := http.Get(url)
	if err != nil {
		log.Printf("Error downloading image from %s: %v\n", url, err)
		return
	}
	defer response.Body.Close()

	// Crear una carpeta "images" si no existe
	err = os.MkdirAll("images", os.ModePerm)
	if err != nil {
		log.Printf("Error creating directory: %v\n", err)
		return
	}

	// Crear el archivo local para almacenar la imagen
	file, err := os.Create("images/" + filename)
	if err != nil {
		log.Printf("Error creating file: %v\n", err)
		return
	}
	defer file.Close()

	// Escribir el contenido de la imagen en el archivo local
	_, err = io.Copy(file, response.Body)
	if err != nil {
		log.Printf("Error writing image to file: %v\n", err)
		return
	}
}

// Función para obtener precios y cambios desde la API de Binance
func getPricesAndChanges() (map[string]string, map[string]string) {
	prices := make(map[string]string)
	changes := make(map[string]string)

	// Obtener precios
	resp, err := http.Get("https://api.binance.com/api/v3/ticker/price")
	if err != nil {
		log.Printf("Error fetching prices: %v\n", err)
		return prices, changes
	}
	defer resp.Body.Close()

	var priceData []BinancePriceInfo
	if err := json.NewDecoder(resp.Body).Decode(&priceData); err != nil {
		log.Printf("Error decoding price data: %v\n", err)
		return prices, changes
	}

	for _, item := range priceData {
		prices[item.Symbol] = item.Price
	}

	// Obtener cambios de 24 horas
	resp, err = http.Get("https://api.binance.com/api/v3/ticker/24hr")
	if err != nil {
		log.Printf("Error fetching 24h change: %v\n", err)
		return prices, changes
	}
	defer resp.Body.Close()

	var changeData []BinanceTickerInfo
	if err := json.NewDecoder(resp.Body).Decode(&changeData); err != nil {
		log.Printf("Error decoding change data: %v\n", err)
		return prices, changes
	}

	for _, item := range changeData {
		changes[item.Symbol] = formatChange(item.PriceChangePercent)
	}

	return prices, changes
}

// Función para formatear el cambio de 24 horas con el signo +/-
func formatChange(change string) string {
	changeValue, err := strconv.ParseFloat(change, 64)
	if err != nil {
		log.Printf("Error parsing change value: %v\n", err)
		return change
	}
	if changeValue > 0 {
		return fmt.Sprintf("+%.2f%%", changeValue)
	}
	return fmt.Sprintf("%.2f%%", (changeValue))
}

