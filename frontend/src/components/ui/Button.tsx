import type { ButtonHTMLAttributes, ReactNode } from "react";

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: "primary" | "secondary" | "destructive" | "ghost";
  size?: "default" | "compact";
  children: ReactNode;
}

export function Button({ variant = "primary", size = "default", className = "", children, ...props }: ButtonProps) {
  return <button className={`button ${variant} ${size} ${className}`} {...props}>{children}</button>;
}
