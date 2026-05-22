package types

type Signup_password_req struct {
	Agreed          bool   `json:"agreed"`
	ConfirmPassword string `json:"confirmPassword"`
	Email           string `json:"email"`
	Name            string `json:"name"`
	Password        string `json:"password"`
}
