import { SafeAreaView, StyleSheet, Text, View } from "react-native";

export default function Home() {
  return (
    <SafeAreaView style={styles.safe}>
      <View style={styles.container}>
        <Text style={styles.title}>Marketplace</Text>
        <Text style={styles.body}>Scaffold is up. The feed lands in Phase 2.</Text>
      </View>
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  safe: { flex: 1 },
  container: {
    flex: 1,
    justifyContent: "center",
    // paddingHorizontal, not paddingLeft/Right — the latter does not mirror
    // under RTL and produces visibly lopsided Arabic layouts.
    paddingHorizontal: 24,
    gap: 8,
  },
  title: { fontSize: 28, fontWeight: "600" },
  body: { fontSize: 16, opacity: 0.7 },
});
