export default function ErrorBanner({ message }: { message: string | null }) {
  if (!message) return null;
  return <p style={{ color: "#dc2626" }}>{message}</p>;
}
