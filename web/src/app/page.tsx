import Providers from "../app/providers";
import { Dashboard } from "../components/Dashboard";

export const ssg = true;

export default function HomePage() {
  return (
    <Providers>
      <Dashboard />
    </Providers>
  );
}
