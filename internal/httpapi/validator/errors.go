package validator

type FieldErrors map[string][]string

func addFieldError(fields FieldErrors, field, message string) FieldErrors {
	if fields == nil {
		fields = make(FieldErrors)
	}
	fields[field] = append(fields[field], message)
	return fields
}
