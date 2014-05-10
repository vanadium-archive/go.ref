package rt

import (
	"veyron2/product"
)

// Product wraps the product.T interface so that we can add functions
// representing the Runtime
type Product struct{ product.T }

func (_ Product) RuntimeOpt() {}
