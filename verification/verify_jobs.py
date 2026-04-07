import os
import time
from playwright.sync_api import sync_playwright, expect

def verify_jobs_page():
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        context = browser.new_context()
        page = context.new_page()

        try:
            # 1. Register and login to get a session
            print("Registering user...")
            page.goto("http://localhost:8080")
            # Usually there's a redirect to /login or /register if not authenticated
            # Let's try to register a test user directly if possible, or use the form

            # Since I don't know the exact UI for login/register, I'll use the API if I can
            # or just look at the page content.

            # Based on memory, I might need to register.
            # Let's try to just go to /jobs and see what happens.
            page.goto("http://localhost:8080/jobs")
            time.sleep(2) # wait for redirect or load

            if "login" in page.url or "register" in page.url:
                print("Redirected to auth. Registering...")
                # Fill registration form if it exists
                # This is a guess based on standard patterns
                try:
                    page.get_by_label("Email").fill("bolt@example.com")
                    page.get_by_label("Password").fill("password123")
                    page.get_by_role("button", name="Register").click()
                except:
                    # Try login if registration fails or isn't there
                    page.get_by_label("Email").fill("bolt@example.com")
                    page.get_by_label("Password").fill("password123")
                    page.get_by_role("button", name="Login").click()

                time.sleep(2)
                page.goto("http://localhost:8080/jobs")

            # 2. Wait for the jobs list to load
            print("Waiting for jobs list...")
            # The template has id="jobs-list"
            expect(page.locator("#jobs-list")).to_be_visible()

            # 3. Take a screenshot
            page.screenshot(path="verification/jobs_page.png")
            print("Screenshot saved to verification/jobs_page.png")

        except Exception as e:
            print(f"Error during verification: {e}")
            page.screenshot(path="verification/error.png")
        finally:
            browser.close()

if __name__ == "__main__":
    verify_jobs_page()
