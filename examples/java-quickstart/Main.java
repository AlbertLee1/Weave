// Weave Java quickstart — a 5 minute hello-world.
//
// Talks to a local Weave server (or weave-mock fixture) over its REST API
// using java.net.http (JDK 11+, no third-party dependencies). Build:
//
//   javac Main.java
//   WEAVE_BASE_URL=http://localhost:9117 java Main
//
// For a fully-typed Java SDK, run:
//
//   weave-cli sdk gen --lang java --ontology <api-name>
//
// and add the generated Maven artifact to your pom.xml.

import java.net.URI;
import java.net.URLEncoder;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

public final class Main {
    public static void main(String[] args) throws Exception {
        String baseUrl = envOr("WEAVE_BASE_URL", "http://localhost:9117");
        String token = System.getenv("WEAVE_TOKEN");
        new Quickstart(baseUrl, token).run();
    }

    private static String envOr(String key, String fallback) {
        String v = System.getenv(key);
        return (v == null || v.isEmpty()) ? fallback : v;
    }
}

final class Quickstart {
    private final String baseUrl;
    private final String token;
    private final HttpClient http;

    Quickstart(String baseUrl, String token) {
        this.baseUrl = baseUrl.replaceAll("/+$", "");
        this.token = token;
        this.http = HttpClient.newBuilder().connectTimeout(Duration.ofSeconds(10)).build();
    }

    void run() throws Exception {
        System.out.println("=== Ontologies ===");
        List<Map<String, String>> ontologies = parseDataObjects(get("/api/v2/ontologies"));
        for (Map<String, String> o : ontologies) {
            System.out.println("- " + o.getOrDefault("apiName", "?") + "\t" + o.getOrDefault("displayName", "?"));
        }
        if (ontologies.isEmpty()) {
            System.out.println("(no ontologies — load a fixture e.g. testdata/northwind to see more)");
            return;
        }

        String ontology = ontologies.get(0).get("apiName");
        System.out.println("=== Object types in " + ontology + " ===");
        String typesPath = "/api/v2/ontologies/" + urlEncode(ontology) + "/objectTypes";
        List<Map<String, String>> types = parseDataObjects(get(typesPath));
        for (Map<String, String> t : types) {
            System.out.println("- " + t.getOrDefault("apiName", "?") + "\t" + t.getOrDefault("displayName", "?"));
        }
        if (types.isEmpty()) {
            return;
        }

        String objectType = types.get(0).get("apiName");
        System.out.println("=== First 5 " + objectType + " ===");
        String objectsPath = "/api/v2/ontologies/" + urlEncode(ontology)
                + "/objects/" + urlEncode(objectType) + "?pageSize=5";
        String body = get(objectsPath);
        for (String row : splitDataObjects(body)) {
            String pk = extractValue(row, "__primaryKey");
            System.out.println("- " + (pk == null ? "?" : pk) + "\t" + row);
        }
    }

    private String get(String path) throws Exception {
        HttpRequest.Builder b = HttpRequest.newBuilder()
                .uri(URI.create(baseUrl + path))
                .timeout(Duration.ofSeconds(10))
                .header("Accept", "application/json")
                .GET();
        if (token != null && !token.isEmpty()) {
            b.header("Authorization", "Bearer " + token);
        }
        HttpResponse<String> resp = http.send(b.build(), HttpResponse.BodyHandlers.ofString());
        if (resp.statusCode() / 100 != 2) {
            throw new RuntimeException("weave " + resp.statusCode() + ": " + resp.body());
        }
        return resp.body();
    }

    private static String urlEncode(String v) {
        return URLEncoder.encode(v, StandardCharsets.UTF_8);
    }

    /**
     * Tiny ad-hoc JSON walker. Returns each top-level object inside the
     * "data" array of a {@code {"data":[...]}} response, as a map of the
     * string fields encountered at the object's top level.
     *
     * <p>Pure stdlib — the JDK has no JSON parser. Only handles the shapes
     * the quickstart asks for: an outer {@code {"data":[{...},{...}]}} with
     * scalar string properties on each row. Anything more complex is
     * preserved as a raw substring rather than parsed.
     */
    private static List<Map<String, String>> parseDataObjects(String json) {
        List<Map<String, String>> out = new ArrayList<>();
        for (String obj : splitDataObjects(json)) {
            out.add(extractStringFields(obj));
        }
        return out;
    }

    private static List<String> splitDataObjects(String json) {
        List<String> out = new ArrayList<>();
        int dataIdx = json.indexOf("\"data\"");
        if (dataIdx < 0) {
            return out;
        }
        int arrayStart = json.indexOf('[', dataIdx);
        if (arrayStart < 0) {
            return out;
        }
        int i = arrayStart + 1;
        int end = json.length();
        boolean inString = false;
        boolean escape = false;
        int depth = 0;
        int objStart = -1;
        while (i < end) {
            char c = json.charAt(i);
            if (inString) {
                if (escape) {
                    escape = false;
                } else if (c == '\\') {
                    escape = true;
                } else if (c == '"') {
                    inString = false;
                }
                i++;
                continue;
            }
            if (c == '"') {
                inString = true;
            } else if (c == '{') {
                if (depth == 0) {
                    objStart = i;
                }
                depth++;
            } else if (c == '}') {
                depth--;
                if (depth == 0 && objStart >= 0) {
                    out.add(json.substring(objStart, i + 1));
                    objStart = -1;
                }
            } else if (c == ']' && depth == 0) {
                break;
            }
            i++;
        }
        return out;
    }

    private static Map<String, String> extractStringFields(String objLiteral) {
        Map<String, String> out = new LinkedHashMap<>();
        Pattern p = Pattern.compile("\"([^\"\\\\]+)\"\\s*:\\s*\"((?:[^\"\\\\]|\\\\.)*)\"");
        Matcher m = p.matcher(objLiteral);
        while (m.find()) {
            out.put(m.group(1), unescape(m.group(2)));
        }
        return out;
    }

    private static String extractValue(String objLiteral, String key) {
        Map<String, String> all = extractStringFields(objLiteral);
        return all.get(key);
    }

    private static String unescape(String s) {
        StringBuilder b = new StringBuilder(s.length());
        for (int i = 0; i < s.length(); i++) {
            char c = s.charAt(i);
            if (c == '\\' && i + 1 < s.length()) {
                char n = s.charAt(++i);
                switch (n) {
                    case '"': b.append('"'); break;
                    case '\\': b.append('\\'); break;
                    case '/': b.append('/'); break;
                    case 'n': b.append('\n'); break;
                    case 'r': b.append('\r'); break;
                    case 't': b.append('\t'); break;
                    default: b.append('\\').append(n);
                }
            } else {
                b.append(c);
            }
        }
        return b.toString();
    }
}
